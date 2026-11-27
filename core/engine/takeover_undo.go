package engine

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

// TakeoverUndo answers takeover.undo: put the setup back as it was
// before takeover.adopt.
//
//  1. write the exact pre-takeover copy (the plan dir's live-backup.yaml)
//     back over the live config and reload mihomo; mihomo keeps running;
//  2. the stored controller secret becomes the one in that copy again (a
//     controller.secure since the adopt is undone with it);
//  3. remove the subscription and the layers adopt imported; layers made
//     after the adopt are kept but turned off, so nothing re-applies;
//  4. forget the adoption (settings.adopted), mark the plan undone.
//
// The service definition was never changed by adopt and stays; history
// and plan backups stay, so the person can take over again later. A
// hardened setup (mihomo moved to Twinnel's own service) is refused: moving
// it back is part of uninstalling the helper.
func (e *Engine) TakeoverUndo(ctx context.Context, p rpc.TakeoverUndoParams) (rpc.TakeoverUndoResult, error) {
	res := rpc.TakeoverUndoResult{DryRun: p.DryRun, Steps: []rpc.TakeoverUndoStep{}}
	if !e.adoptMu.TryLock() {
		return res, rpc.Errorf(rpc.CodeBusy, "another takeover operation is running")
	}
	defer e.adoptMu.Unlock()
	st, err := e.Store.Settings()
	if err != nil {
		return res, err
	}
	ad := st.Adopted
	if ad == nil {
		return res, rpc.ErrInvalid("%s: Twinnel has not taken over this setup", rpc.UndoNotAdopted)
	}
	if st.Hardened != nil {
		return res, rpc.ErrInvalid("%s: mihomo runs from Twinnel's own service (since harden); uninstall the helper with the move back instead", rpc.UndoHardened)
	}
	res.PlanID = ad.PlanID
	backup, err := os.ReadFile(ad.Backup)
	if err != nil {
		return res, rpc.ErrInvalid("%s: the pre-takeover copy %s cannot be read: %v", rpc.UndoNoBackup, ad.Backup, err)
	}
	if _, err := config.Parse(backup); err != nil {
		return res, rpc.ErrInvalid("%s: the pre-takeover copy %s is not a config: %v", rpc.UndoNoBackup, ad.Backup, err)
	}

	// What adopt imported: the plan's base and user layers.
	imported := map[string]bool{}
	dir, plan, ps, perr := e.loadPlan(ad.PlanID)
	if perr == nil {
		for _, l := range plan.Layers {
			if l.Stage == config.StageBase.String() || l.Stage == config.StageUser.String() {
				imported[l.ID] = true
			}
		}
	}
	var remove, disable []string
	for _, l := range st.Layers {
		switch {
		case imported[l.ID]:
			remove = append(remove, l.ID)
		case l.Enabled:
			disable = append(disable, l.ID)
		}
	}
	hasSub := false
	for _, s := range st.Subscriptions {
		hasSub = hasSub || s.ID == AdoptedSubscriptionID
	}
	oldSecret, _ := topLevel(backup, "secret")
	secretDiffers := e.Secrets != nil && e.controllerSecret() != oldSecret

	planned := func(code, path string, n int) {
		res.Steps = append(res.Steps, rpc.TakeoverUndoStep{Code: code, Path: path, Count: n, Status: "planned"})
	}
	planned(rpc.UndoRestoreConfig, ad.LiveConfig, 0)
	planned(rpc.UndoReload, "", 0)
	if secretDiffers {
		planned(rpc.UndoRestoreSecret, "", 0)
	}
	if hasSub {
		planned(rpc.UndoRemoveSub, "", 0)
	}
	if len(remove) > 0 {
		planned(rpc.UndoRemoveLayers, "", len(remove))
	}
	if len(disable) > 0 {
		planned(rpc.UndoDisableLayers, "", len(disable))
	}
	if st.Mihomo.ServiceLabel != "" && st.Mihomo.ServiceAdopted {
		planned(rpc.UndoKeepService, st.Mihomo.ServiceLabel, 0)
	}
	planned(rpc.UndoKeepData, "", 0)
	if p.DryRun {
		res.OK = true
		return res, nil
	}

	mark := func(code, status, detail string) {
		for i := range res.Steps {
			if res.Steps[i].Code == code {
				res.Steps[i].Status, res.Steps[i].Detail = status, detail
			}
		}
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(ad.LiveConfig); err == nil {
		mode = fi.Mode().Perm()
	}
	e.adopting.Store(true) // no auto-apply while the store changes
	defer e.adopting.Store(false)
	if err := e.restoreExact(ctx, ad.LiveConfig, backup, mode, store.HistoryMeta{Reason: "undo takeover " + ad.PlanID, Code: store.ReasonTakeoverUndo, Args: []string{ad.PlanID}}); err != nil {
		mark(rpc.UndoRestoreConfig, "failed", err.Error())
		res.Error = "restore: " + err.Error()
		return res, nil
	}
	mark(rpc.UndoRestoreConfig, "ok", "written back byte for byte")
	mark(rpc.UndoReload, "ok", "")

	var errs []error
	if secretDiffers {
		var err error
		if oldSecret == "" {
			err = e.Secrets.Delete(store.SecretController)
		} else {
			err = e.Secrets.Set(store.SecretController, oldSecret)
		}
		if err != nil {
			mark(rpc.UndoRestoreSecret, "failed", err.Error())
			errs = append(errs, err)
		} else {
			mark(rpc.UndoRestoreSecret, "ok", "")
		}
	}
	for _, id := range remove {
		if err := e.Store.DeleteLayer(id); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if hasSub {
		if err := e.Store.DeleteSubscription(AdoptedSubscriptionID); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
		if e.Secrets != nil {
			if err := e.Secrets.Delete(store.SubscriptionURLKey(AdoptedSubscriptionID)); err != nil && !errors.Is(err, store.ErrNoSecret) {
				errs = append(errs, err)
			}
		}
	}
	rm := map[string]bool{}
	for _, id := range remove {
		rm[id] = true
	}
	off := map[string]bool{}
	for _, id := range disable {
		off[id] = true
	}
	_, err = e.Store.UpdateSettings(func(st *store.Settings) error {
		layers := st.Layers[:0]
		for _, l := range st.Layers {
			if rm[l.ID] {
				continue
			}
			if off[l.ID] {
				l.Enabled = false
			}
			layers = append(layers, l)
		}
		st.Layers = layers
		subs := st.Subscriptions[:0]
		for _, s := range st.Subscriptions {
			if s.ID != AdoptedSubscriptionID {
				subs = append(subs, s)
			}
		}
		st.Subscriptions = subs
		st.Adopted = nil
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	for _, code := range []string{rpc.UndoRemoveSub, rpc.UndoRemoveLayers, rpc.UndoDisableLayers, rpc.UndoKeepService, rpc.UndoKeepData} {
		mark(code, "ok", "")
	}
	if perr == nil {
		ps.State = rpc.PlanUndone
		if err := writePlanState(dir, ps); err != nil {
			e.log().Warn("undo: plan state", "err", err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		res.Error = fmt.Sprintf("the config is back, but cleaning up Twinnel's copy failed: %v", err)
	} else {
		res.OK = true
	}
	e.log().Info("takeover undone", "plan", ad.PlanID, "ok", res.OK)
	e.publish(rpc.EventTakeoverChanged, map[string]any{"plan_id": ad.PlanID, "state": rpc.PlanUndone})
	e.publish(rpc.EventStateChanged, nil)
	return res, nil
}
