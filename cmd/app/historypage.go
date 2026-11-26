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
	diffID  string
	diff    *editor.DiffState
	diffErr string
	result  *rpc.ApplyResult
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
	if len(d.history) == 0 {
		card(c, "", func() { ui.Text(c, T.HistoryEmpty).TextColor(t.TextMuted) })
		return
	}
	// Newest first.
	hist := make([]rpc.HistoryEntry, len(d.history))
	for i, e := range d.history {
		hist[len(hist)-1-i] = e
	}
	cols := []ui.TableColumn{
		{Title: T.HistoryTime, Width: 150},
		{Title: T.HistoryKind, Width: 80},
		{Title: T.HistorySize, Width: 80},
		{Title: T.HistoryHash, Width: 110},
		{Title: T.HistoryReason},
	}
	v.list.RowHeight = 30
	ui.Table(c, &v.list, cols, len(hist), func(row, col int) {
		e := hist[row]
		cell := ui.Text
		sel := e.ID == v.sel
		var el ui.Element
		switch col {
		case 0:
			el = cell(c, e.Time.Local().Format("01-02 15:04:05"))
		case 1:
			el = cell(c, T.HistoryKindName(e.Kind))
		case 2:
			el = cell(c, status.Bytes(int64(e.Size)))
		case 3:
			el = cell(c, shortHash(e.Hash)).Font("monospace").FontSize(12)
		case 4:
			el = cell(c, e.Reason)
		}
		el.SingleLine()
		if sel {
			el.Bold()
		}
		if el.Clicked() {
			v.sel = e.ID
		}
	}).Height(float32(min(len(hist), 10)*30 + 40))
	if v.sel == "" {
		ui.Text(c, T.HistoryPick).FontSize(12).TextColor(t.TextMuted)
	}
	var entry *rpc.HistoryEntry
	for i := range hist {
		if hist[i].ID == v.sel {
			entry = &hist[i]
		}
	}
	if entry == nil {
		return
	}
	card(c, "", func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, T.HistoryEntry).FontSize(14).Bold()
			ui.Text(c, entry.ID).Font("monospace").FontSize(12).TextColor(t.TextMuted).Selectable()
			ui.Spacer(c)
			if ui.Button(c.Key("hist-diff"), T.HistoryDiff).Disabled(a.hd.busy != "").Clicked() {
				a.historyDiff(s, entry.ID)
			}
			if iconButton(c.Key("hist-rollback"), iconRefresh, T.HistoryRollback, true).Disabled(!a.helperWrites(s)).Clicked() {
				a.historyRollback(*entry)
			}
		})
		kv(c, T.HistoryTime, entry.Time.Local().Format("2006-01-02 15:04:05"))
		kv(c, T.HistoryKind, T.HistoryKindName(entry.Kind))
		if entry.Reason != "" {
			kv(c, T.HistoryReason, entry.Reason)
		}
		if v.diffErr != "" && v.diffID == entry.ID {
			ui.Text(c, v.diffErr).FontSize(12).TextColor(t.TextMuted).Wrap()
		}
		if v.diff != nil && v.diffID == entry.ID {
			add, del := v.diff.Stats()
			ui.Text(c, T.HistoryDiffHint+" · "+fmt.Sprintf(T.DiffStats, add, del)).FontSize(12).TextColor(t.TextMuted)
			if add+del > 0 {
				editor.DiffView(c.Key("hist-diff-view"), v.diff).Height(380).Radius(8).Border(1, t.Border)
			}
		}
		if r := v.result; r != nil {
			applySummary(c, *r)
		}
	})
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
		if old, err = fetchConfig(ctx, hc, cfgPath, "history", id); err != nil {
			return err
		}
		live, err = fetchConfig(ctx, hc, cfgPath, "live", "")
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
	})
}
