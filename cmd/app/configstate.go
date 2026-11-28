package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// What the 订阅与覆写 page knows about the running config, beyond the
// overrides: the entry mihomo runs (the newest applied or rolled-back
// history entry), the overrides that differ from it (U39), and the drift
// after a rollback (U41).

// lastApplied is the newest history entry mihomo was given (applied or
// rolled back), nil when there is none.
func lastApplied(hist []rpc.HistoryEntry) *rpc.HistoryEntry {
	for i := range hist {
		if hist[i].Kind != "backup" {
			return &hist[i]
		}
	}
	return nil
}

// historyByID finds an entry.
func historyByID(hist []rpc.HistoryEntry, id string) *rpc.HistoryEntry {
	for i := range hist {
		if hist[i].ID == id {
			return &hist[i]
		}
	}
	return nil
}

// layerChange is one override that differs from the applied set.
type layerChange struct {
	id, name string
	added    bool // not in the applied set
	removed  bool // in the applied set, deleted since
	nowOn    bool // on now
	wasOn    bool // on in the applied set
	content  bool // its text differs
}

// pendingChanges lists the overrides that differ from the applied
// manifest m: stored layers (content, switch, new, deleted), the managed
// layers' switches, the leak-guard groups and the active subscription.
// sub reports whether the active subscription differs.
func pendingChanges(m *rpc.LayerManifest, cur []rpc.Layer, set *rpc.Settings, activeSub string) (ch []layerChange, sub bool) {
	if m == nil {
		return nil, false
	}
	then := map[string]rpc.ManifestLayer{}
	for _, l := range m.Layers {
		then[l.ID] = l
	}
	for _, l := range cur {
		c := layerChange{id: l.ID, name: layerTitle(l), nowOn: l.Enabled}
		switch {
		case l.ID == coexistLayerID:
			c.wasOn = m.Coexist
		case l.Managed: // leak guard: its groups
			c.wasOn = len(m.LeakGuard) > 0
			if set != nil && strings.Join(set.LeakGuardGroups, "\n") != strings.Join(m.LeakGuard, "\n") && c.nowOn && c.wasOn {
				c.content = true
			}
		default:
			ml, ok := then[l.ID]
			if !ok {
				c.added = true
				if !l.Enabled {
					continue // a new layer that is off changes nothing
				}
				break
			}
			c.wasOn = ml.Enabled
			c.content = l.Hash != "" && ml.Hash != l.Hash && (l.Enabled || ml.Enabled)
			delete(then, l.ID)
		}
		if c.added || c.nowOn != c.wasOn || c.content {
			ch = append(ch, c)
		}
	}
	for _, ml := range composeOrder(m.Layers) {
		if _, ok := then[ml.ID]; ok {
			ch = append(ch, layerChange{id: ml.ID, name: manifestName(ml), removed: true, wasOn: ml.Enabled})
		}
	}
	sub = m.Subscription != "" && activeSub != "" && m.Subscription != activeSub
	return ch, sub
}

// changeNames are the names a pending row lists.
func changeNames(ch []layerChange) []string {
	T := i18n.T
	out := make([]string, 0, len(ch))
	for _, c := range ch {
		if c.removed {
			out = append(out, i18n.F(T.PendingRemoved, c.name))
		} else {
			out = append(out, c.name)
		}
	}
	return out
}

// changeReason is the 说明 an apply of these changes gets by default
// (H6): 启用 X, 停用 X, 改动 X, or 改动 N 个覆写.
func changeReason(ch []layerChange) string {
	T := i18n.T
	switch len(ch) {
	case 0:
		return ""
	case 1:
		c := ch[0]
		switch {
		case c.nowOn && !c.wasOn && !c.removed:
			return i18n.F(T.ReasonEnable, c.name)
		case !c.nowOn && c.wasOn || c.removed:
			return i18n.F(T.ReasonDisable, c.name)
		}
		return i18n.F(T.ReasonEdit, c.name)
	}
	return i18n.F(T.ReasonEditN, len(ch))
}

// activeSubscription is the ID of the subscription in use, "" for none.
func (a *app) activeSubscription() string {
	for _, s := range a.hd.subs {
		if s.Active {
			return s.ID
		}
	}
	return ""
}

// pendingText is the apply card's state while the composed config
// differs from what mihomo runs (U39): what differs, by name. last is the
// entry mihomo runs, ch and sub what differs from it, for 放弃改动.
func (a *app) pendingText() (text string, last *rpc.HistoryEntry, ch []layerChange, sub bool) {
	d := &a.hd
	T := i18n.T
	last = lastApplied(d.history)
	if last != nil {
		ch, sub = pendingChanges(last.Layers, d.layers, d.settings, a.activeSubscription())
	}
	names := changeNames(ch)
	if sub {
		names = append([]string{T.PendingSub}, names...)
	}
	text = T.PendingUnknownRow
	if len(names) > 0 {
		text = i18n.F(T.PendingRow, len(names), strings.Join(names, T.ListSep))
	}
	return text, last, ch, sub
}

// discardChanges sets the overrides back to the applied entry's
// (layer.restore) at once; its toast offers 撤销 (kubai R-8).
func (a *app) discardChanges(last rpc.HistoryEntry) {
	T := i18n.T
	a.restoreLayers(T.Discard, T.DoneDiscarded, last.ID, nil)
}

// restoreLayers runs layer.restore as a reversible action; the editor
// drops what it held for the layers set back.
func (a *app) restoreLayers(label, doneText, historyID string, ids []string) {
	a.reversibleOp(label, doneText, func(ctx context.Context, hc *status.HelperConn) error {
		var r rpc.LayerRestoreResult
		return hc.Call(ctx, rpc.MethodLayerRestore, rpc.LayerRestoreParams{HistoryID: historyID, IDs: ids}, &r)
	}, func(err error) {
		v := &a.hd.cfg
		if err == nil && v.sel != "" && !v.isNew {
			v.ed = nil // reloaded from the helper
			if i := indexLayer(a.hd.layers, v.sel); i >= 0 {
				a.loadLayerContent(v.sel)
			}
		}
	})
}

func indexLayer(ls []rpc.Layer, id string) int {
	for i, l := range ls {
		if l.ID == id {
			return i
		}
	}
	return -1
}

// revertable reports whether a stored layer differs from the applied
// entry's version of it, which 还原为已应用版本 then brings back.
func (a *app) revertable(l rpc.Layer) (*rpc.HistoryEntry, bool) {
	last := lastApplied(a.hd.history)
	if last == nil || last.Layers == nil || l.Managed {
		return nil, false
	}
	for _, ml := range last.Layers.Layers {
		if ml.ID == l.ID {
			return last, ml.Hash != l.Hash || ml.Enabled != l.Enabled
		}
	}
	return nil, false
}

func (a *app) revertLayer(l rpc.Layer) {
	T := i18n.T
	last, ok := a.revertable(l)
	if !ok {
		return
	}
	a.restoreLayers(T.LayerRevert, i18n.F(T.DoneLayerReverted, layerTitle(l)), last.ID, []string{l.ID})
}

// driftOf returns the drift mark the helper keeps after a rollback.
func driftOf(d *helperData) *rpc.Drift {
	if d.settings == nil {
		return nil
	}
	return d.settings.Drift
}

// driftState is the apply card's state while mihomo runs a history
// entry the overrides do not compose (U41): which one, its ⓘ, and the
// two ways back. Drawn in the card's first row.
func (a *app) driftState(c *ui.Context, s status.Snapshot, dr rpc.Drift, w bool) {
	d := &a.hd
	t := c.Theme()
	T := i18n.T
	e := historyByID(d.history, dr.HistoryID)
	when := dr.Since
	if tt, ok := i18n.HistoryIDTime(dr.HistoryID); ok {
		when = tt
	}
	if e != nil {
		when = e.Time
	}
	text := T.DriftRow
	if !dr.Manifest {
		text = T.DriftRowBackup
	}
	ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(pal(t).warnText).Shrink(0)
	// Two short lines, not one long one the buttons squeeze into a
	// mid-word wrap (walkthrough 2, N9).
	ui.Column(c).Gap(gapSub).Grow(1).Shrink(1).MinWidth(0).Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf(text, when.Local().Format("01-02 15:04"))).Shrink(1).SingleLine()
			infoTip(c, "", T.DriftInfo, nil).Shrink(0)
		})
		small(c, T.DriftRowSub).SingleLine().Shrink(1)
	})
	busy := d.busy != ""
	// What 恢复 does to each override is in the ⓘ; it happens at once and
	// its toast offers 撤销 (kubai R-8).
	if dr.Manifest && pushButton(c.Key("drift-restore"), T.DriftRestore).Disabled(!w || busy).Clicked() {
		a.restoreLayers(T.DriftRestore, T.DoneRestored, dr.HistoryID, nil)
	}
	if a.opAction(c.Key("drift-reapply"), iconOK, T.DriftReapply, true, !w || busy, T.Apply).Clicked() {
		a.runApply()
	}
}

// adoptedSubID is the subscription takeover makes from the live config
// (core/engine AdoptedSubscriptionID).
const adoptedSubID = "adopted"

// adoptedBase is the file name of the base takeover used, when the
// takeover page has loaded the adopted plan.
func (a *app) adoptedBase() string {
	if v := &a.hd.take; !v.loaded && !v.loading {
		a.loadTakeover() // a read: the plans name the base
	}
	for _, p := range a.hd.take.plans {
		if p.State == rpc.PlanAdopted && p.Base != "" {
			return filepath.Base(p.Base)
		}
	}
	return ""
}

// activeSubscriptionName is the name of the subscription in use.
func (a *app) activeSubscriptionName() string {
	for _, s := range a.hd.subs {
		if s.Active {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		}
	}
	return ""
}
