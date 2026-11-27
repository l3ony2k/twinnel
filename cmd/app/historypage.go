package main

import (
	"context"
	"fmt"
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
	cols := []ui.TableColumn{
		{Title: T.HistoryTime, Width: 150},
		{Title: T.HistoryKind, Width: 80},
		{Title: T.HistorySize, Width: 80},
		{Title: T.HistoryHash, Width: 110},
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
				el = cell(c, e.Time.Local().Format("01-02 15:04:05"))
			case 1:
				el = cell(c, T.HistoryKindName(e.Kind))
			case 2:
				el = cell(c, status.Bytes(int64(e.Size)))
			case 3:
				el = cell(c, shortHash(e.Hash)).Font("monospace").FontSize(plat.calloutSize)
			case 4:
				el = cell(c, e.Reason).Tooltip(e.Reason)
			}
			el.SingleLine()
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
		ui.Text(c, entry.ID).Font("monospace").FontSize(plat.calloutSize).TextColor(t.TextMuted).Selectable()
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
		if entry.Reason != "" {
			kv(c, T.HistoryReason, entry.Reason)
		}
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
			applySummary(c, *r)
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
	a.confirm(fmt.Sprintf(T.HistoryRollbackTitle, when), T.HistoryRollbackBody, T.HistoryRollback, func() {
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
			if err == nil {
				v.result = &res
				v.diff, v.diffID = nil, ""
			}
		})
	}
}
