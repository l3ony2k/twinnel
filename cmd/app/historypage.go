package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// The 历史 page: applied configs, a diff of one against the live config,
// rollback.

type historyView struct {
	list    ui.ListState
	sel     string
	selRow  int // the table's chosen row (list.Selected), kept in step with sel
	diffID  string
	diff    *editor.DiffState
	diffErr string
	result  *rpc.ApplyResult
	reveal  bool // bring the detail into view in the next frame
}

func (a *app) historyPage(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	d := &a.hd
	v := &d.hist
	pageTitle(c, T.PageHistory)
	if !a.helperGate(c, s) {
		return
	}
	a.opNotes(c, s)
	if a.helperListsLoading(c, 3) {
		return // no false 还没有历史记录 before the first load
	}
	if len(d.history) == 0 {
		groupBox(c, t).Children(func() { emptyState(c, iconHistory, T.HistoryEmpty, "") })
		return
	}
	hist := d.history // newest first, as history.list returns it
	// No hash column (U40): it is in the detail, copyable.
	cols := []ui.TableColumn{
		{Title: T.HistoryTime, Width: 150},
		{Title: T.HistoryKind, Width: 80},
		{Title: T.HistorySize, Width: 80},
		{Title: T.HistoryReason},
	}
	v.list.RowHeight = 30
	// The chosen row is the table's own selection (the accent row, as
	// every macOS table), which the arrow keys also move.
	v.selRow = -1
	for i := range hist {
		if hist[i].ID == v.sel {
			v.selRow = i
		}
	}
	v.list.Selected, v.list.RoundRows = &v.selRow, true
	card(c, "", func() {
		tbl := ui.Table(c, &v.list, cols, len(hist), func(row, col int) {
			e := hist[row]
			cell := ui.Text
			var el ui.Element
			switch col {
			case 0:
				el = num(cell(c, e.Time.Local().Format("01-02 15:04:05")))
			case 1:
				el = cell(c, T.HistoryKindName(e.Kind))
			case 2:
				el = monoText(c, status.Bytes(int64(e.Size)))
			case 3:
				reason := historyReason(e)
				el = ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if e.BeforeDuet {
						tag(c, T.HistBeforeDuet)
					}
					cell(c, reason).SingleLine().Shrink(1).Tooltip(reason)
				})
			}
			if col != 3 {
				el.SingleLine()
			}
			el.ContextMenu(func(m *ui.Menu) {
				if m.Item(T.HistoryDiff).Disabled(a.hd.busy != "").Chosen() {
					v.sel, v.reveal = e.ID, true
					a.historyDiff(s, e.ID)
				}
				if m.Item(T.HistoryRollback + "…").Disabled(!a.helperWrites(s)).Chosen() {
					v.sel = e.ID
					a.historyRollback(e)
				}
				m.Separator()
				if m.Item(T.CopyID).Chosen() {
					copyText(c, e.ID)
				}
				if m.Item(T.HistCopyHash).Chosen() {
					copyText(c, e.Hash)
				}
			})
		})
		tbl.Height(float32(min(len(hist), 10)*30 + 40)).Background(ui.Transparent).MarginX(-8)
		if tbl.Changed() && v.selRow >= 0 && v.selRow < len(hist) && hist[v.selRow].ID != v.sel {
			v.sel, v.reveal = hist[v.selRow].ID, true
		}
		if v.sel == "" {
			small(c, T.HistoryPick)
		}
	})
	var entry *rpc.HistoryEntry
	for i := range hist {
		if hist[i].ID == v.sel {
			entry = &hist[i]
		}
	}
	if entry == nil {
		return
	}
	detail := headedCard(c, func() {
		ui.Text(c, T.HistoryEntry).FontSize(plat.headingSize).FontWeight(plat.headingWeight)
		infoTipAbout(c, T.HistoryRollback, T.TipHistRollback, nil)
		if entry.BeforeDuet {
			tag(c, T.HistBeforeDuet)
		}
		ui.Spacer(c)
		if pushButton(c.Key("hist-diff"), T.HistoryDiff).Disabled(a.hd.busy != "").Clicked() {
			a.historyDiff(s, entry.ID)
		}
		if a.opButton(c.Key("hist-rollback"), iconRollback, T.HistoryRollback+"…", false, T.HistoryRollback).Disabled(!a.helperWrites(s)).Clicked() {
			a.historyRollback(*entry)
		}
	}, func() {
		kv(c, T.HistoryTime, entry.Time.Local().Format("2006-01-02 15:04:05"))
		kv(c, T.HistoryKind, T.HistoryKindName(entry.Kind))
		if r := historyReason(*entry); r != "" {
			kv(c, T.HistoryReason, r)
		}
		if m := entry.Layers; m != nil {
			kvWith(c, T.HistOverrides, func() {
				ui.Text(c, manifestNames(m)).Grow(1).Wrap().LineHeight(lineText)
			})
		}
		// The hash and the ID are identifiers: quiet, copyable (U40).
		kvWith(c, T.HistoryHash, func() {
			monoText(c, shortHash(entry.Hash)).TextColor(t.TextMuted).
				Tooltip(entry.Hash).Selectable().ContextMenu(func(m *ui.Menu) {
				if m.Item(T.HistCopyHash).Chosen() {
					copyText(c, entry.Hash)
				}
				if m.Item(T.CopyID).Chosen() {
					copyText(c, entry.ID)
				}
			})
			meta(c, status.Bytes(int64(entry.Size)))
		})
		if v.diffErr != "" && v.diffID == entry.ID {
			ui.Text(c, v.diffErr).FontSize(plat.calloutSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText)
		}
		if v.diff != nil && v.diffID == entry.ID {
			add, del := v.diff.Stats()
			ui.Text(c, T.HistoryDiffHint+" · "+fmt.Sprintf(T.DiffStats, add, del)).FontSize(plat.calloutSize).TextColor(t.TextMuted)
			if add+del > 0 {
				codeFrame(c, 380, func() { editor.DiffView(c.Key("hist-diff-view"), v.diff).Grow(1) })
			}
		}
		if r := v.result; r != nil {
			a.applySummary(c, *r)
		}
	})
	// The detail sits under the table: bring it into view as a row is
	// chosen, so it never opens off-screen (review-b 015 #2).
	if v.reveal {
		v.reveal = false
		detail.ScrollIntoView(ui.ScrollIntoViewOptions{Animation: &rowMove})
	}
}

func (a *app) historyDiff(s status.Snapshot, id string) {
	T := i18n.T
	cfgPath := ""
	if st := s.Helper.Status; st != nil {
		cfgPath = st.Mihomo.ConfigPath
	}
	var old, live string
	a.helperOp(T.HistoryDiff, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		var err error
		if old, err = fetchConfig(ctx, hc, cfgPath, rpc.ConfigHistory, id); err != nil {
			return err
		}
		live, err = fetchConfig(ctx, hc, cfgPath, rpc.ConfigLive, "")
		return err
	}, func(err error) {
		v := &a.hd.hist
		v.diffID, v.diff, v.diffErr = id, nil, ""
		if err != nil {
			a.hd.msg, a.hd.msgErr = "", false
			v.diffErr = fmt.Sprintf(T.DiffUnavailable, helperError(err))
			return
		}
		v.diff = newDiff(old, live)
	})
}

func (a *app) historyRollback(e rpc.HistoryEntry) {
	T := i18n.T
	when := e.Time.Local().Format("2006-01-02 15:04:05")
	// The confirm says what the rollback does to the overrides (U41):
	// which turn off or on, or that they will not describe it at all.
	body := T.HistoryRollbackBody
	if lines := rollbackEffects(e.Layers, a.hd.layers); len(lines) > 0 {
		body += "\n\n" + strings.Join(lines, "\n")
	}
	a.confirm(fmt.Sprintf(T.HistoryRollbackTitle, when), body, T.HistoryRollback, func() {
		a.historyRollbackNow(e)
	})
}

// historyRollbackNow rolls back without asking (after the confirm).
func (a *app) historyRollbackNow(e rpc.HistoryEntry) {
	T := i18n.T
	{
		var res rpc.ApplyResult
		a.helperOp(T.HistoryRollback, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
			return hc.Call(ctx, rpc.MethodHistoryRollback, rpc.IDParams{ID: e.ID}, &res)
		}, func(err error) {
			v := &a.hd.hist
			v.result = nil
			if err != nil {
				if v.result = failedResult(err); v.result != nil {
					a.hd.msg, a.hd.msgErr = "", false
				}
			}
			if err == nil {
				v.result = &res
				v.diff, v.diffID = nil, ""
			}
		})
	}
}

// historyReason words an entry's reason (U40): the helper's code in the
// UI language, the user's own words as written.
func historyReason(e rpc.HistoryEntry) string {
	return i18n.T.HistoryReasonText(e.ReasonCode, e.ReasonArgs, e.Reason)
}

// manifestName is a manifest layer's name in the UI language.
func manifestName(l rpc.ManifestLayer) string {
	return i18n.T.TakeoverLayerName(l.NameCode, l.NameArgs, l.Name)
}

// manifestNames lists the overrides that were on in a manifest, in
// compose order, the managed ones last.
func manifestNames(m *rpc.LayerManifest) string {
	T := i18n.T
	var on []string
	for _, l := range composeOrder(m.Layers) {
		if l.Enabled {
			on = append(on, manifestName(l))
		}
	}
	if m.Coexist {
		on = append(on, T.StageName("coexist"))
	}
	if len(m.LeakGuard) > 0 {
		on = append(on, T.StageName("guard"))
	}
	if len(on) == 0 {
		return "—"
	}
	return strings.Join(on, T.ListSep)
}

// composeOrder is the stored layers in the order compose uses: base
// first, then user, each in their saved order (tech.md §3).
func composeOrder(ls []rpc.ManifestLayer) []rpc.ManifestLayer {
	out := slices.Clone(ls)
	slices.SortStableFunc(out, func(a, b rpc.ManifestLayer) int {
		return stageRank(a.Stage) - stageRank(b.Stage)
	})
	return out
}

func stageRank(stage string) int {
	switch stage {
	case stageBase:
		return 0
	case stageUser:
		return 1
	}
	return 2
}

// rollbackEffects says what rolling back to an entry composed from target
// does to the overrides now (cur, the layer list with the managed rows):
// which turn off, which on, which have other content. A nil target (a
// backup, an older entry) is a config the overrides do not describe.
func rollbackEffects(target *rpc.LayerManifest, cur []rpc.Layer) []string {
	T := i18n.T
	if target == nil {
		return []string{T.RollbackNoLayers}
	}
	then := map[string]rpc.ManifestLayer{}
	for _, l := range target.Layers {
		then[l.ID] = l
	}
	var off, on, diff []string
	for _, l := range cur {
		wasOn, hash := false, ""
		switch {
		case l.ID == coexistLayerID:
			wasOn = target.Coexist
		case l.Managed: // leak guard
			wasOn = len(target.LeakGuard) > 0
		default:
			if m, ok := then[l.ID]; ok {
				wasOn, hash = m.Enabled, m.Hash
			}
		}
		switch {
		case l.Enabled && !wasOn:
			off = append(off, layerTitle(l))
		case !l.Enabled && wasOn:
			on = append(on, layerTitle(l))
		case l.Enabled && !l.Managed && hash != "" && l.Hash != "" && hash != l.Hash:
			diff = append(diff, layerTitle(l))
		}
		delete(then, l.ID)
	}
	for _, m := range composeOrder(target.Layers) { // deleted since
		if _, ok := then[m.ID]; ok && m.Enabled {
			on = append(on, manifestName(m))
		}
	}
	var out []string
	if len(off) > 0 {
		out = append(out, i18n.F(T.RollbackDisables, strings.Join(off, T.ListSep)))
	}
	if len(on) > 0 {
		out = append(out, i18n.F(T.RollbackEnables, strings.Join(on, T.ListSep)))
	}
	if len(diff) > 0 {
		out = append(out, i18n.F(T.RollbackChanges, strings.Join(diff, T.ListSep)))
	}
	return out
}
