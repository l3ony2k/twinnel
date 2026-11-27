package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
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

// pendingRow is the 预览与应用 card's row while the composed config
// differs from what mihomo runs (U39): what differs, by name, with
// 预览 and 放弃改动. Nothing when nothing is pending.
func (a *app) pendingRow(c *ui.Context, s status.Snapshot, w bool) {
	d := &a.hd
	if !d.unapplied || driftOf(d) != nil {
		return
	}
	t := c.Theme()
	T := i18n.T
	last := lastApplied(d.history)
	var ch []layerChange
	var subChanged bool
	if last != nil {
		ch, subChanged = pendingChanges(last.Layers, d.layers, d.settings, a.activeSubscription())
	}
	names := changeNames(ch)
	if subChanged {
		names = append([]string{T.PendingSub}, names...)
	}
	text := T.PendingUnknownRow
	if len(names) > 0 {
		text = i18n.F(T.PendingRow, len(names), strings.Join(names, T.ListSep))
	}
	ui.Row(c.Key("pending")).Gap(8).AlignItems(ui.Center).Children(func() {
		dot(c, t.Accent, 8)
		ui.Text(c, text).Grow(1).Shrink(1).Wrap().LineHeight(lineText)
		canDiscard := last != nil && last.Layers != nil && (len(ch) > 0 || subChanged)
		if canDiscard && pushButton(c.Key("discard"), T.Discard+"…").Disabled(!w).Clicked() {
			a.discardChanges(*last, ch)
		}
	})
}

// discardChanges asks, then sets the overrides back to the applied
// entry's (layer.restore).
func (a *app) discardChanges(last rpc.HistoryEntry, ch []layerChange) {
	T := i18n.T
	var restore, remove []string
	for _, c := range ch {
		if c.added {
			remove = append(remove, c.name)
		} else {
			restore = append(restore, c.name)
		}
	}
	var lines []string
	if len(restore) > 0 {
		lines = append(lines, i18n.F(T.DiscardRestores, strings.Join(restore, T.ListSep)))
	}
	if len(remove) > 0 {
		lines = append(lines, i18n.F(T.DiscardRemoves, strings.Join(remove, T.ListSep)))
	}
	a.confirm(T.DiscardTitle, strings.Join(lines, "\n"), T.Discard, func() {
		a.restoreLayers(T.Discard, last.ID, nil)
	})
}

// restoreLayers runs layer.restore; the editor drops what it held for
// the layers set back.
func (a *app) restoreLayers(label, historyID string, ids []string) {
	a.helperOp(label, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
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
	a.confirm(i18n.F(T.LayerRevertTitle, layerTitle(l)), "", T.LayerRevert, func() {
		a.restoreLayers(T.LayerRevert, last.ID, []string{l.ID})
	})
}

// driftOf returns the drift mark the helper keeps after a rollback.
func driftOf(d *helperData) *rpc.Drift {
	if d.settings == nil {
		return nil
	}
	return d.settings.Drift
}

// driftRow is the top of the 订阅与覆写 page while mihomo runs a history
// entry the overrides do not compose (U41): which one, and the two ways
// back.
func (a *app) driftRow(c *ui.Context, s status.Snapshot) {
	d := &a.hd
	dr := driftOf(d)
	if dr == nil {
		return
	}
	t := c.Theme()
	T := i18n.T
	w := a.helperWrites(s)
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
	groupBox(c, t).Key("drift").Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(pal(t).warnText)
			ui.Text(c, fmt.Sprintf(text, when.Local().Format("01-02 15:04"))).Grow(1).Shrink(1).Wrap().LineHeight(lineText)
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spacer(c)
			if dr.Manifest && pushButton(c.Key("drift-restore"), T.DriftRestore+"…").Disabled(!w).Clicked() {
				id := dr.HistoryID
				var lines []string
				if e != nil {
					// Restoring the entry's overrides is the rollback's
					// effect on them, said the same way.
					lines = rollbackEffects(e.Layers, d.layers)
				}
				body := strings.Join(append(lines, T.DriftRestoreBody), "\n")
				a.confirm(i18n.F(T.AskF, T.DriftRestore), body, T.DriftRestore, func() {
					a.restoreLayers(T.DriftRestore, id, nil)
				})
			}
			if a.opButton(c.Key("drift-reapply"), iconOK, T.DriftReapply, false, T.Apply).Disabled(!w).Clicked() {
				a.runApply()
			}
		})
	})
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

// groupLabel heads a compose group of the 覆写 list (U33).
func groupLabel(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(plat.smallSize).FontWeight(600).TextColor(c.Theme().TextMuted).Padding(8, 0, 0).SingleLine()
}
