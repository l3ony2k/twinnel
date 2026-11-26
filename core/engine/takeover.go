package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/config"
	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/store"
	"github.com/l3ony2k/duet/core/takeover"
)

// AdoptChecks are the health checks compared before and after an adopt.
// If any of them is worse afterwards, the adopt is rolled back.
var AdoptChecks = []string{"magicdns", "tailnet-resolver-binding", "tailscale-direct-rules", "tailnet-route", "system-dns"}

// AdoptedSubscriptionID is the id of the subscription an adopt imports
// (the plan's base file).
const AdoptedSubscriptionID = "adopted"

// Files in a plan dir besides those core/takeover writes.
const (
	planStateFile  = "plan.state.json"
	planBackupFile = "live-backup.yaml"
)

// planState is the helper's bookkeeping for one plan dir.
type planState struct {
	PlanID      string `json:"plan_id"`
	State       string `json:"state"`
	ConfirmHash string `json:"confirm_hash"`
	BasePath    string `json:"base_path"`
	BaseSHA256  string `json:"base_sha256"`
	// Profile whose URL adopt stores (only when exactly one was found).
	Profile    *takeover.Profile `json:"profile,omitempty"`
	AdoptedAt  time.Time         `json:"adopted_at,omitzero"`
	Backup     string            `json:"backup,omitempty"`
	LastResult *rpc.AdoptResult  `json:"last_result,omitempty"`
	// Impact of adopt's reload: live config → composed config.
	Impact *rpc.ReloadImpact `json:"impact,omitempty"`
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func (e *Engine) readPlanState(dir string) (*planState, error) {
	b, err := os.ReadFile(filepath.Join(dir, planStateFile))
	if err != nil {
		return nil, err
	}
	ps := &planState{}
	if err := json.Unmarshal(b, ps); err != nil {
		return nil, err
	}
	return ps, nil
}

func writePlanState(dir string, ps *planState) error {
	b, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	return store.WriteFileAtomic(filepath.Join(dir, planStateFile), append(b, '\n'), 0o600)
}

// confirmHash ties an adopt to the exact plan.json the client was shown.
func confirmHash(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "plan.json"))
	if err != nil {
		return "", err
	}
	return sha256Hex(b)[:16], nil
}

func (e *Engine) takeoverProbe() (takeover.Probe, error) {
	if e.TakeoverProbe == nil {
		return nil, rpc.Errorf(rpc.CodeFailed, "takeover is not supported by this helper (no platform probe)")
	}
	return e.TakeoverProbe, nil
}

// TakeoverPlan inspects the running setup (read-only) and stores a plan
// under <state>/takeover/<plan_id>/.
func (e *Engine) TakeoverPlan(ctx context.Context, p rpc.TakeoverPlanParams) (rpc.TakeoverPlan, error) {
	probe, err := e.takeoverProbe()
	if err != nil {
		return rpc.TakeoverPlan{}, err
	}
	if !e.adoptMu.TryLock() {
		return rpc.TakeoverPlan{}, rpc.Errorf(rpc.CodeBusy, "an adopt is running")
	}
	defer e.adoptMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	opt := takeover.Options{DiscoverOptions: e.TakeoverDiscover}
	if p.Base != "" {
		// Only a candidate the helper found itself: a root helper must not
		// import an arbitrary file a client names.
		disc, err := takeover.Discover(ctx, probe, e.TakeoverDiscover)
		if err != nil {
			return rpc.TakeoverPlan{}, rpc.Errorf(rpc.CodeFailed, "%v", err)
		}
		want, _ := filepath.Abs(p.Base)
		ok := false
		for _, c := range takeover.FindCandidates(disc.Mihomo.Config) {
			if a, _ := filepath.Abs(c); a == want {
				ok = true
			}
		}
		if !ok {
			return rpc.TakeoverPlan{}, rpc.ErrInvalid("base %q is not one of the candidates next to the live config", p.Base)
		}
		opt.BasePath = want
	}
	plan, err := takeover.BuildPlan(ctx, probe, opt)
	if err != nil {
		return rpc.TakeoverPlan{}, rpc.Errorf(rpc.CodeFailed, "takeover plan: %v", err)
	}

	stamp := e.now().UTC().Format("20060102-150405")
	var dir, id string
	for i := 1; ; i++ {
		id = "plan-" + stamp + "-" + plan.LiveHash[:8]
		if i > 1 {
			id += fmt.Sprintf("-%d", i)
		}
		dir, err = e.Store.CreateTakeoverDir(id)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || i > 20 {
			return rpc.TakeoverPlan{}, err
		}
	}
	fail := func(err error) (rpc.TakeoverPlan, error) {
		os.RemoveAll(dir)
		return rpc.TakeoverPlan{}, err
	}
	m := plan.Discovery.Mihomo
	ps := &planState{PlanID: id, State: rpc.PlanReady}
	if plan.Base != nil {
		ps.BasePath = plan.Base.Path
		b, err := os.ReadFile(plan.Base.Path)
		if err != nil {
			return fail(err)
		}
		ps.BaseSHA256 = sha256Hex(b)
	}
	switch n := len(m.Profiles); {
	case n == 1:
		pr := m.Profiles[0]
		ps.Profile = &pr
	case n > 1:
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d subscription profiles found; adopt does not import a subscription URL (set one later with `duet sub put`)", n))
	}
	if live, err := os.ReadFile(plan.Live); err == nil && len(plan.Composed()) > 0 {
		im := ReloadImpact(live, plan.Composed())
		ps.Impact = &im
	}
	if err := plan.WriteFiles(dir); err != nil {
		return fail(err)
	}
	if ps.ConfirmHash, err = confirmHash(dir); err != nil {
		return fail(err)
	}
	if err := writePlanState(dir, ps); err != nil {
		return fail(err)
	}
	e.log().Info("takeover plan", "id", id, "equivalent", plan.Equivalent, "live", plan.Live)
	e.publish(rpc.EventTakeoverChanged, map[string]any{"plan_id": id, "state": ps.State})
	return planSummary(plan, ps, true), nil
}

func planSummary(p *takeover.Plan, ps *planState, full bool) rpc.TakeoverPlan {
	out := rpc.TakeoverPlan{
		PlanID: ps.PlanID, CreatedAt: p.CreatedAt, State: ps.State, ConfirmHash: ps.ConfirmHash,
		LiveConfig: p.Live, LiveSHA256: p.LiveHash, Equivalent: p.Equivalent,
		Validated: p.Validation.Ran && p.Validation.OK, ValidateErr: p.Validation.Errors,
		SubscriptionURL: ps.Profile != nil, AdoptedAt: ps.AdoptedAt, Backup: ps.Backup,
		Layers: []rpc.TakeoverLayer{},
	}
	if p.Base != nil {
		out.Base = p.Base.Path
	}
	if m := p.Discovery.Mihomo; m != nil {
		out.MihomoBinary, out.MihomoVersion, out.Controller, out.SecretSet = m.Binary, m.Version, m.Controller, m.SecretSet
		if m.Service != nil {
			out.MihomoService = m.Service.Label
		}
	}
	if t := p.Discovery.Tailscale; t != nil && t.Service != nil {
		out.TailscaleSvc = t.Service.Label
	}
	for _, c := range p.Candidates {
		out.Candidates = append(out.Candidates, rpc.TakeoverCandidate{
			Path: c.Path, Size: c.Size, ProxiesSame: c.ProxiesSame, ProxiesTotal: c.ProxiesTotal,
			GroupsSame: c.GroupsSame, GroupsTotal: c.GroupsTotal, Rules: c.Rules, CommonTail: c.CommonTail,
			HeadRules: c.HeadRules, SettingsDiffer: c.SettingsDiffer, Explains: c.Explains, Chosen: c.Chosen,
			Error: config.RedactText(c.Error),
		})
	}
	for _, l := range p.Layers {
		out.Layers = append(out.Layers, rpc.TakeoverLayer{ID: l.ID, Name: l.Name, Kind: string(l.Kind), Stage: l.Stage, Keys: l.Keys, Rules: l.Rules})
	}
	for _, c := range p.Changes {
		out.Changes = append(out.Changes, rpc.TakeoverChange{Area: c.Area, What: c.What, Deliberate: c.Deliberate, Why: c.Why})
	}
	for _, w := range p.Warnings {
		out.Warnings = append(out.Warnings, config.RedactText(w))
	}
	if im := ps.Impact; im != nil {
		out.Impact = im
		if im.Disruptive {
			out.Warnings = append(out.Warnings, "adopt's reload interrupts the network briefly: "+strings.Join(im.Notes, "; "))
		}
	}
	if full {
		out.Summary = p.Text()
		if p.SemDiff != nil {
			out.SemDiff = p.SemDiff.Text(80)
		}
	}
	return out
}

// loadPlan reads a plan dir.
func (e *Engine) loadPlan(id string) (string, *takeover.Plan, *planState, error) {
	if !store.ValidID(id) {
		return "", nil, nil, rpc.ErrInvalid("invalid plan_id %q", id)
	}
	dir, _ := e.Store.TakeoverDir(id)
	ps, err := e.readPlanState(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, nil, rpc.ErrNotFound("takeover plan", id)
	}
	if err != nil {
		return "", nil, nil, rpc.Errorf(rpc.CodeFailed, "plan %s: %v", id, err)
	}
	plan, err := takeover.LoadPlan(filepath.Join(dir, "plan.json"))
	if err != nil {
		return "", nil, nil, rpc.Errorf(rpc.CodeFailed, "plan %s: %v", id, err)
	}
	return dir, plan, ps, nil
}

// TakeoverPlans lists stored plans, newest first (without the texts).
func (e *Engine) TakeoverPlans() ([]rpc.TakeoverPlan, error) {
	ids, err := e.Store.TakeoverIDs()
	if err != nil {
		return nil, err
	}
	out := []rpc.TakeoverPlan{}
	for i := len(ids) - 1; i >= 0; i-- {
		_, plan, ps, err := e.loadPlan(ids[i])
		if err != nil {
			out = append(out, rpc.TakeoverPlan{PlanID: ids[i], State: "broken", Warnings: []string{err.Error()}})
			continue
		}
		out = append(out, planSummary(plan, ps, false))
	}
	return out, nil
}

// TakeoverGet returns one plan with its texts.
func (e *Engine) TakeoverGet(id string) (rpc.TakeoverPlan, error) {
	_, plan, ps, err := e.loadPlan(id)
	if err != nil {
		return rpc.TakeoverPlan{}, err
	}
	return planSummary(plan, ps, true), nil
}

// TakeoverDiscard deletes a plan dir. The plan that was adopted holds the
// exact live backup and is kept unless forced.
func (e *Engine) TakeoverDiscard(p rpc.TakeoverDiscardParams) error {
	if !store.ValidID(p.PlanID) {
		return rpc.ErrInvalid("invalid plan_id %q", p.PlanID)
	}
	if !e.adoptMu.TryLock() {
		return rpc.Errorf(rpc.CodeBusy, "an adopt is running")
	}
	defer e.adoptMu.Unlock()
	dir, _ := e.Store.TakeoverDir(p.PlanID)
	ps, err := e.readPlanState(dir)
	if errors.Is(err, fs.ErrNotExist) {
		if _, serr := os.Stat(dir); serr != nil {
			return rpc.ErrNotFound("takeover plan", p.PlanID)
		}
	}
	if ps != nil && ps.State == rpc.PlanAdopted && !p.Force {
		return rpc.ErrInvalid("plan %s was adopted and holds the backup of the live config (%s); pass force to delete it anyway", p.PlanID, ps.Backup)
	}
	if err := e.Store.RemoveTakeoverDir(p.PlanID); err != nil {
		return err
	}
	e.publish(rpc.EventTakeoverChanged, map[string]any{"plan_id": p.PlanID, "state": "discarded"})
	return nil
}

// storeUndo reverts what an adopt imported into the store.
type storeUndo struct {
	settings *store.Settings
	subs     []string
	layers   []string
	secrets  []string
}

func (e *Engine) undoStore(u *storeUndo) error {
	var errs []error
	if _, err := e.Store.UpdateSettings(func(st *store.Settings) error { *st = *u.settings; return nil }); err != nil {
		errs = append(errs, err)
	}
	for _, id := range u.subs {
		errs = append(errs, e.Store.DeleteSubscription(id))
	}
	for _, id := range u.layers {
		errs = append(errs, e.Store.DeleteLayer(id))
	}
	if e.Secrets != nil {
		for _, k := range u.secrets {
			errs = append(errs, e.Secrets.Delete(k))
		}
	}
	return errors.Join(errs...)
}

// Adopt takes over the existing installation described by a plan:
//
//  1. check confirm_hash, re-discover, and refuse if the live config (or the
//     base) changed since the plan;
//  2. keep an exact copy of the live config in the plan dir;
//  3. import the base as subscription "adopted" (and its URL into Secrets
//     when the plan found exactly one profile), the derived layers, and
//     settings pointing at the adopted binary, config, controller and
//     service labels (marked adopted: their plists are never replaced);
//     coexist on, leak guard off;
//  4. run AdoptChecks against the live setup;
//  5. compose.apply (backup → mihomo -t → write → reload → verify);
//  6. run AdoptChecks again; if any is worse (rechecked once after a short
//     wait), write the exact copy back, reload, and undo the store import.
//
// A refusal before step 2 is an RPC error. Later failures return a result
// with OK false and the steps taken.
func (e *Engine) Adopt(ctx context.Context, p rpc.AdoptParams) (rpc.AdoptResult, error) {
	res := rpc.AdoptResult{PlanID: p.PlanID, Steps: []rpc.AdoptStep{}}
	step := func(name, status, detail string) {
		res.Steps = append(res.Steps, rpc.AdoptStep{Name: name, Status: status, Detail: detail})
		e.log().Info("adopt", "plan", p.PlanID, "step", name, "status", status, "detail", detail)
	}
	if !store.ValidID(p.PlanID) {
		return res, rpc.ErrInvalid("invalid plan_id %q", p.PlanID)
	}
	probe, err := e.takeoverProbe()
	if err != nil {
		return res, err
	}
	if !e.adoptMu.TryLock() {
		return res, rpc.Errorf(rpc.CodeBusy, "another takeover operation is running")
	}
	defer e.adoptMu.Unlock()

	dir, plan, ps, err := e.loadPlan(p.PlanID)
	if err != nil {
		return res, err
	}
	if ps.State == rpc.PlanAdopted {
		return res, rpc.ErrInvalid("plan %s is already adopted", p.PlanID)
	}
	if ch, err := confirmHash(dir); err != nil || p.ConfirmHash == "" || p.ConfirmHash != ps.ConfirmHash || ch != ps.ConfirmHash {
		return res, rpc.ErrInvalid("confirm_hash does not match plan %s; show the plan again and confirm", p.PlanID)
	}
	if !plan.Equivalent {
		return res, rpc.ErrInvalid("plan %s has UNEXPECTED differences from the live config; review them and re-plan", p.PlanID)
	}
	if plan.Validation.Ran && !plan.Validation.OK {
		return res, rpc.ErrInvalid("mihomo -t rejected the plan's composed config: %s", strings.Join(plan.Validation.Errors, "; "))
	}
	if e.healthSched() == nil && !p.SkipHealth {
		return res, rpc.ErrInvalid("health checks are not available in this helper, so a bad adopt could not be detected; pass skip_health to adopt anyway")
	}
	st, err := e.Store.Settings()
	if err != nil {
		return res, err
	}
	if len(st.Layers) > 0 || len(st.Subscriptions) > 0 {
		return res, rpc.ErrInvalid("adopt needs a helper without subscriptions or layers (found %d and %d); remove them first", len(st.Subscriptions), len(st.Layers))
	}

	// ---- 1. the live setup still matches the plan ----
	dctx, cancel := context.WithTimeout(ctx, time.Minute)
	disc, err := takeover.Discover(dctx, probe, e.TakeoverDiscover)
	cancel()
	if err != nil {
		return res, rpc.Errorf(rpc.CodeFailed, "discover: %v", err)
	}
	m := disc.Mihomo
	if m.Config != plan.Live {
		return res, rpc.Errorf(rpc.CodeFailed, "the live config is now %s, the plan was made for %s; re-plan", m.Config, plan.Live)
	}
	live, err := os.ReadFile(plan.Live)
	if err != nil {
		return res, rpc.Errorf(rpc.CodeFailed, "read live config: %v", err)
	}
	if sha256Hex(live) != plan.LiveHash {
		return res, rpc.Errorf(rpc.CodeFailed, "live config changed since the plan was made; re-plan")
	}
	base, err := os.ReadFile(ps.BasePath)
	if err != nil || sha256Hex(base) != ps.BaseSHA256 {
		return res, rpc.Errorf(rpc.CodeFailed, "the plan's base %s changed or is gone; re-plan", ps.BasePath)
	}
	liveSecret, _ := topLevel(live, "secret")
	if stored := e.controllerSecret(); stored != "" && stored != liveSecret {
		return res, rpc.ErrInvalid("the helper already holds a controller secret that the live config does not use; adopt would add it to the config. Adopt keeps the live secret; use controller.secure afterwards")
	}
	fi, err := os.Stat(plan.Live)
	if err != nil {
		return res, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	liveMode := fi.Mode().Perm()
	step("verify", "ok", fmt.Sprintf("live config %s unchanged since the plan (sha256 %s)", plan.Live, short(plan.LiveHash)))

	e.adopting.Store(true)
	defer e.adopting.Store(false)
	finish := func(state string) (rpc.AdoptResult, error) {
		ps.State = state
		r := res
		ps.LastResult = &r
		if err := writePlanState(dir, ps); err != nil {
			e.log().Warn("adopt: plan state", "err", err)
		}
		e.publish(rpc.EventTakeoverChanged, map[string]any{"plan_id": p.PlanID, "state": state, "rolled_back": res.RolledBack})
		return res, nil
	}

	// ---- 2. exact backup ----
	backup := filepath.Join(dir, planBackupFile)
	if err := store.WriteFileAtomic(backup, live, 0o600); err != nil {
		step("backup", "failed", err.Error())
		res.Error = "backup: " + err.Error()
		return finish(rpc.PlanFailed)
	}
	res.Backup = backup
	step("backup", "ok", backup)

	// ---- 3. import ----
	undo := &storeUndo{settings: st}
	imported, err := e.importPlan(plan, ps, disc, base, liveSecret, backup, undo)
	if err != nil {
		step("import", "failed", err.Error())
		if uerr := e.undoStore(undo); uerr != nil {
			step("undo import", "failed", uerr.Error())
		}
		res.Error = "import: " + err.Error()
		return finish(rpc.PlanFailed)
	}
	step("import", "ok", imported)

	// ---- 4. health before ----
	var before map[string]health.Status
	if !p.SkipHealth {
		var checks []rpc.AdoptCheck
		checks, before = e.adoptHealth(ctx)
		res.HealthBefore = checks
		step("health before", "ok", checkLine(checks))
	} else {
		step("health before", "skipped", "skip_health")
	}

	// ---- 5. apply ----
	start := e.now()
	ar, aerr := e.Apply(ctx, rpc.ApplyParams{Reason: "adopt " + p.PlanID, Force: true})
	res.Apply = ar
	if aerr != nil {
		step("apply", "failed", aerr.Error())
		// Apply restores the previous file itself; make sure it is byte-exact.
		if cur, err := os.ReadFile(plan.Live); err != nil || sha256Hex(cur) != plan.LiveHash {
			if rerr := e.restoreExact(ctx, plan.Live, live, liveMode, "adopt "+p.PlanID+" failed"); rerr != nil {
				step("restore", "failed", rerr.Error())
			} else {
				step("restore", "ok", "live config written back")
			}
		}
		if uerr := e.undoStore(undo); uerr != nil {
			step("undo import", "failed", uerr.Error())
		} else {
			step("undo import", "ok", "settings, layers and subscription as before")
		}
		res.RolledBack = ar != nil && ar.RolledBack
		res.Error = "apply: " + aerr.Error()
		return finish(rpc.PlanFailed)
	}
	res.HistoryID = ar.HistoryID
	res.BackupHistoryID = e.backupSince(start)
	step("apply", "ok", fmt.Sprintf("written=%v reloaded=%v history %s", ar.Written, ar.Reloaded, ar.HistoryID))
	res.Warnings = append(res.Warnings, ar.Warnings...)
	if comp := plan.Composed(); len(comp) > 0 {
		if stripped, _, err := withoutSecret(comp); err == nil && store.Hash(stripped) != ar.Hash {
			res.Warnings = append(res.Warnings, "the applied config differs from the plan's composed.yaml (the tailnet peers or interface changed since the plan)")
		}
	}

	// ---- 6. health after ----
	if !p.SkipHealth {
		settle := e.AdoptSettle
		if settle == 0 {
			settle = 3 * time.Second
		}
		var after map[string]health.Status
		var worse []string
		for try := 0; try < 2; try++ {
			if settle > 0 {
				select {
				case <-time.After(settle):
				case <-ctx.Done():
				}
			}
			res.HealthAfter, after = e.adoptHealth(ctx)
			if worse = worseChecks(before, after); len(worse) == 0 {
				break
			}
		}
		res.Worse = worse
		if len(worse) > 0 {
			step("health after", "failed", "worse: "+strings.Join(worse, ", ")+"; "+checkLine(res.HealthAfter))
			if rerr := e.restoreExact(ctx, plan.Live, live, liveMode, "adopt "+p.PlanID+" rolled back"); rerr != nil {
				step("rollback", "failed", rerr.Error())
				res.Error = fmt.Sprintf("health got worse after adopt (%s) and the rollback failed: %v; restore %s by hand", strings.Join(worse, ", "), rerr, backup)
			} else {
				res.RolledBack = true
				step("rollback", "ok", "live config written back byte for byte and reloaded")
				res.Error = fmt.Sprintf("health got worse after adopt (%s); rolled back to the live config", strings.Join(worse, ", "))
			}
			if uerr := e.undoStore(undo); uerr != nil {
				step("undo import", "failed", uerr.Error())
			} else {
				step("undo import", "ok", "settings, layers and subscription as before")
			}
			res.HealthAfterRollback, _ = e.adoptHealth(ctx)
			return finish(rpc.PlanRolledBack)
		}
		step("health after", "ok", checkLine(res.HealthAfter))
	} else {
		step("health after", "skipped", "skip_health")
	}

	res.OK = true
	ps.AdoptedAt, ps.Backup = e.now().UTC(), backup
	e.triggerHealth("adopted")
	return finish(rpc.PlanAdopted)
}

// importPlan writes the subscription, the layers, secrets and settings.
// Everything it creates is recorded in undo.
func (e *Engine) importPlan(plan *takeover.Plan, ps *planState, disc *takeover.Discovery, base []byte, liveSecret, backup string, undo *storeUndo) (string, error) {
	m := disc.Mihomo
	if err := e.Store.WriteSubscription(AdoptedSubscriptionID, base); err != nil {
		return "", err
	}
	undo.subs = append(undo.subs, AdoptedSubscriptionID)
	var ids []string
	var layers []store.Layer
	for _, l := range plan.Layers {
		if l.Stage != config.StageBase.String() && l.Stage != config.StageUser.String() {
			continue // the managed coexist layer is generated by the helper
		}
		if l.ID == CoexistLayerID || l.ID == LeakGuardLayerID {
			return "", fmt.Errorf("layer id %q is reserved", l.ID)
		}
		if err := e.Store.WriteLayer(l.ID, l.Data()); err != nil {
			return "", err
		}
		undo.layers = append(undo.layers, l.ID)
		ids = append(ids, l.ID)
		layers = append(layers, store.Layer{ID: l.ID, Name: l.Name, Stage: l.Stage, Format: store.FormatMerge, Enabled: true})
	}
	withURL := false
	subName := "Adopted: " + filepath.Base(ps.BasePath)
	if ps.Profile != nil && e.Secrets != nil {
		u, err := takeover.ProfileURL(*ps.Profile)
		if err != nil {
			return "", fmt.Errorf("subscription URL: %w", err)
		}
		key := store.SubscriptionURLKey(AdoptedSubscriptionID)
		if err := e.Secrets.Set(key, u); err != nil {
			return "", fmt.Errorf("store the subscription URL: %w", err)
		}
		undo.secrets = append(undo.secrets, key)
		withURL = true
		if ps.Profile.Name != "" {
			subName = ps.Profile.Name
		}
	}
	if liveSecret != "" && e.Secrets != nil && e.controllerSecret() == "" {
		if err := e.Secrets.Set(store.SecretController, liveSecret); err != nil {
			return "", fmt.Errorf("store the controller secret: %w", err)
		}
		undo.secrets = append(undo.secrets, store.SecretController)
	}
	now := e.now().UTC()
	_, err := e.Store.UpdateSettings(func(st *store.Settings) error {
		st.Mihomo.Binary = m.Binary
		st.Mihomo.ConfigDir = m.Dir
		st.Mihomo.ConfigFile = m.Config
		if m.Controller != "" {
			st.Mihomo.Controller = controllerAddr(m.Controller)
		}
		if m.Service != nil {
			st.Mihomo.ServiceLabel, st.Mihomo.ServiceAdopted = m.Service.Label, true
		}
		if t := disc.Tailscale; t != nil && t.Service != nil {
			st.Tailscale.ServiceLabel, st.Tailscale.ServiceAdopted = t.Service.Label, true
		}
		st.Tailscale.Coexist = true
		st.Tailscale.Resolver = ""
		st.LeakGuardGroups = nil
		st.Subscriptions = append(st.Subscriptions, store.Subscription{
			ID: AdoptedSubscriptionID, Name: subName, Active: true, IntervalMinutes: 0,
			LastChange: now, Hash: store.Hash(base), Size: len(base),
		})
		st.Layers = append(st.Layers, layers...)
		st.Adopted = &store.Adoption{PlanID: ps.PlanID, At: now, LiveConfig: m.Config, Backup: backup}
		return nil
	})
	if err != nil {
		return "", err
	}
	detail := fmt.Sprintf("subscription %s (url=%v), layers %s; settings point at %s", AdoptedSubscriptionID, withURL, strings.Join(ids, ", "), m.Config)
	return detail, nil
}

// adoptHealth runs AdoptChecks now with a fresh Env.
func (e *Engine) adoptHealth(ctx context.Context) ([]rpc.AdoptCheck, map[string]health.Status) {
	s := e.healthSched()
	if s == nil || s.Runner == nil {
		return nil, nil
	}
	r := &health.Runner{Registry: s.Runner.Registry, Timeout: s.Runner.Timeout, Concurrency: s.Runner.Concurrency, Only: AdoptChecks}
	env := s.Env
	if s.NewEnv != nil {
		env = s.NewEnv()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	rep := r.Run(ctx, env)
	out := []rpc.AdoptCheck{}
	m := map[string]health.Status{}
	for _, c := range rep.Checks {
		out = append(out, rpc.AdoptCheck{ID: c.ID, Status: string(c.Status), Summary: c.Summary})
		m[c.ID] = c.Status
	}
	return out, m
}

// adoptRank orders statuses for the before/after comparison. Unlike the
// report's overall status, skip ranks below ok: a check that could read the
// live setup before and can't afterwards is a regression.
func adoptRank(s health.Status) int {
	switch s {
	case health.StatusOK:
		return 0
	case health.StatusSkip, "":
		return 1
	case health.StatusWarn:
		return 2
	}
	return 3
}

func worseChecks(before, after map[string]health.Status) []string {
	var out []string
	for _, id := range AdoptChecks {
		b, okb := before[id]
		a, oka := after[id]
		if !okb || !oka {
			continue
		}
		if adoptRank(a) > adoptRank(b) {
			out = append(out, fmt.Sprintf("%s %s→%s", id, b, a))
		}
	}
	return out
}

func checkLine(cs []rpc.AdoptCheck) string {
	var parts []string
	for _, c := range cs {
		parts = append(parts, c.ID+"="+c.Status)
	}
	return strings.Join(parts, " ")
}

// backupSince returns the newest "backup" history entry made at or after t.
func (e *Engine) backupSince(t time.Time) string {
	idx, err := e.Store.History()
	if err != nil {
		return ""
	}
	best := ""
	var bt time.Time
	for _, h := range idx {
		if h.Kind == store.HistoryBackup && !h.Time.Before(t.Truncate(time.Second)) && (best == "" || h.Time.After(bt)) {
			best, bt = h.ID, h.Time
		}
	}
	return best
}

// restoreExact writes data back to path byte for byte, reloads mihomo if it
// runs and waits for its controller, and records a rollback in history.
func (e *Engine) restoreExact(ctx context.Context, path string, data []byte, mode fs.FileMode, reason string) error {
	e.applyMu.Lock()
	defer e.applyMu.Unlock()
	st, err := e.Store.Settings()
	if err != nil {
		return err
	}
	ctrl := e.currentController(st)
	vctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	_, verr := ctrl.Version(vctx)
	cancel()
	cd, err := openConfDir(filepath.Dir(path), st.Mihomo.ConfigDirUID)
	if err != nil {
		return err
	}
	defer cd.Close()
	name := filepath.Base(path)
	oldInfo, _ := cd.Stat(name)
	force := true
	if cur, err := cd.ReadFile(name); err == nil {
		force = ReloadImpact(cur, data).Force
	}
	if err := writeConfined(cd, name, data, mode, oldInfo); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if verr == nil {
		if err := e.reloadAndVerify(ctx, ctrl, path, force); err != nil {
			return err
		}
	}
	if _, err := e.Store.AddHistory(store.HistoryRollback, reason, stripSecretBytes(data), e.now(), st.HistoryKeep); err != nil {
		e.log().Warn("history", "err", err)
	}
	e.triggerHealth("rollback")
	return nil
}

// SecureController generates a new controller secret, keeps it in the
// secret store and applies it. The previous secret is restored if the apply
// fails. Adopt never does this on its own: other clients of the controller
// (the dashboard, scripts) need the new secret.
func (e *Engine) SecureController(ctx context.Context) (rpc.ControllerSecureResult, error) {
	var out rpc.ControllerSecureResult
	if e.Secrets == nil {
		return out, rpc.Errorf(rpc.CodeFailed, "no secret store")
	}
	st, err := e.Store.Settings()
	if err != nil {
		return out, err
	}
	if _, ok := st.ActiveSubscription(); !ok {
		return out, rpc.ErrInvalid("no active subscription: nothing to apply the secret to")
	}
	old := e.controllerSecret()
	out.Replaced = old != ""
	secret, err := newSecret()
	if err != nil {
		return out, err
	}
	if err := e.Secrets.Set(store.SecretController, secret); err != nil {
		return out, rpc.Errorf(rpc.CodeFailed, "store the secret: %v", err)
	}
	ar, aerr := e.Apply(ctx, rpc.ApplyParams{Reason: "controller.secure", Force: true})
	out.Apply = ar
	if aerr != nil {
		if old != "" {
			err = e.Secrets.Set(store.SecretController, old)
		} else {
			err = e.Secrets.Delete(store.SecretController)
		}
		if err != nil {
			e.log().Error("controller.secure: restore the previous secret", "err", err)
		}
		return out, aerr
	}
	out.Warnings = append(out.Warnings, "clients of the controller (the web dashboard, the app, scripts) need the new secret; it is in the helper's secret store (macOS: sudo security find-generic-password -s duet."+store.SecretController+" -w)")
	return out, nil
}
