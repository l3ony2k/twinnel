package main

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// States that stand in for content: a service that can't be reached, a
// list not loaded yet, configuration not applied yet (kubai 017, 022).

// netService names mihomo or Tailscale for the unreachable states.
type netService int

const (
	svcMihomo netService = iota
	svcTailscale
)

func (s netService) texts() (title, body, short string) {
	T := i18n.T
	if s == svcTailscale {
		return T.UnreachableTSTitle, T.UnreachableTSBody, T.UnreachableTSShort
	}
	return T.UnreachableMihomoTitle, T.UnreachableMihomoBody, T.UnreachableMihomoShort
}

// unreachableState is a page's box when mihomo or Tailscale can't be
// reached: what didn't load, why in plain words, then 重试 and 打开设置;
// the raw error is diagnostic, so it waits behind a 详细信息 disclosure,
// left-aligned under it (unslop R-7). It is a page's state (策略组,
// Tailscale 设备): a box of a page's size with the state in its middle,
// not a short strip at the top of an empty page (kubai R-12); a card
// uses the card-size cardDown (overview.go). Two side by side have the
// same height, so they line up.
func (a *app) unreachableState(c *ui.Context, svc netService, err string) {
	t := c.Theme()
	T := i18n.T
	title, body, _ := svc.texts()
	groupBox(c, t).MinHeight(unreachablePageH).Justify(ui.Center).Children(func() {
		ui.Column(c).Gap(6).AlignItems(ui.Center).Padding(28, 16).Children(func() {
			ui.Icon(c, iconError).FontSize(iconLarge).TextColor(t.TextMuted)
			ui.Text(c, title).FontWeight(500)
			ui.Text(c, body).FontSize(plat.calloutSize).TextColor(t.TextMuted).TextAlign(ui.Center).Wrap().LineHeight(lineText).MaxWidth(420)
			ui.Row(c).Gap(8).Margin(8, 0, 0, 0).Children(func() {
				a.retryButton(c.Key("retry-" + title))
				if pushButton(c.Key("open-settings"), T.OpenSettings).Clicked() {
					a.openPage(pageSettings)
				}
			})
			if err != "" {
				rawDetails(c.Key("raw-"+title), err).MaxWidth(420).Margin(4, 0, 0, 0)
			}
		})
	})
}

// unreachablePageH is the height of a page's unreachable box: about the
// visible part of the content area in the smallest window.
const unreachablePageH = 380

// rawDetails is the raw text of a failure behind a 详细信息 disclosure,
// closed by default: mono, small, selectable, left-aligned and wrapping
// under the link. It is diagnostic; the plain sentence above it is the
// message.
func rawDetails(c *ui.Context, raw string) ui.Element {
	t := c.Theme()
	T := i18n.T
	col := ui.Column(c).Gap(4).AlignItems(ui.Start)
	open := ui.Local(col, "open", func() bool { return false })
	col.Children(func() {
		d := linkDisclosure(c, open != nil && *open, T.UnreachableDetail)
		if d.Clicked() && open != nil {
			*open = !*open
		}
		if open != nil && *open {
			mono(ui.Text(c, raw)).FontSize(plat.smallSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText).
				Selectable().Label(T.UnreachableDetail + "：" + raw)
		}
	})
	return col
}

// failedRow is the failed state at a card's size (kubai R-10, Vibe Design
// Tips 022 d): the warning mark and what did not work (没能检查 mihomo
// 更新), the reason in plain words under it when Twinnel knows one, then 重试
// (its refresh icon turning while retrying) and 复制详情 at the row's end.
// raw is the error as it came; it is never the message. retry may be nil
// (no 重试); busy is true while the retry runs.
func failedRow(c *ui.Context, what, raw string, retry func(), busy bool) ui.Element {
	return failedRowWhy(c, what, i18n.PlainNetError(raw), raw, retry, busy)
}

// failedRowWhy is failedRow with the reason already worded (helperError),
// for a failure whose reason is not only a network one.
func failedRowWhy(c *ui.Context, what, reason, raw string, retry func(), busy bool) ui.Element {
	t := c.Theme()
	T := i18n.T
	row := ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Role(ui.RoleStatus).Label(what)
	row.Children(func() {
		ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(t.Warning).Shrink(0).AccessHidden(true)
		ui.Column(c).Gap(gapSub).Grow(1).Shrink(1).Children(func() {
			ui.Text(c, what).TextColor(pal(t).warnText).Wrap().LineHeight(lineText)
			if reason != "" {
				ui.Text(c, reason).FontSize(plat.calloutSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText)
			}
		})
		if retry != nil {
			b := pushButton(c.Key("failed-retry"), "").Label(T.Retry)
			spin := showBusy(c, b, busy)
			b.Busy(spin).Children(func() {
				turningIcon(c, iconRefresh, spin)
				ui.Text(c, T.Retry).SingleLine()
			})
			if b.Clicked() && !busy {
				retry()
			}
		}
		if raw != "" {
			l := ui.Text(c.Key("failed-copy"), T.TkCopyDetails).FontSize(plat.calloutSize).TextColor(t.Accent).
				SingleLine().Shrink(0).Focusable().Role(ui.RoleButton).Tooltip(raw)
			if l.Clicked() {
				copyText(c, raw)
			}
		}
	})
	return row
}

// unreachableLine is the small form for a card or the flyout: the plain
// sentence, the raw error on hover.
func unreachableLine(c *ui.Context, svc netService, err string) {
	_, _, short := svc.texts()
	e := small(c, short).Wrap()
	if err != "" {
		e.Tooltip(err)
	}
}

// retryButton asks the monitor to check again. The refresh answers in the
// background, so the button turns its icon for busyMin to show it was
// heard; the state below changes by itself when the service answers.
func (a *app) retryButton(c *ui.Context) {
	now := time.Now()
	spin := a.retry.visible(now)
	b := pushFace(c, ui.Button(c, "")).Label(i18n.T.Retry).Busy(spin).Children(func() {
		turningIcon(c, iconRefresh, spin)
		ui.Text(c, i18n.T.Retry).SingleLine()
	})
	if b.Clicked() && !spin {
		a.retry = busySpan{start: now.Add(-busyDelay), end: now}
		time.AfterFunc(busyMin+10*time.Millisecond, func() { mygo.RunOnMain(a.invalidate) })
		a.mon.Refresh()
	}
}

// openPage shows page in the main window (opening it if needed).
func (a *app) openPage(page string) {
	old := a.page
	a.page = page
	a.openMain()
	a.pageChanged(old)
	a.invalidate()
}

// --- not loaded yet ---

// helperListsLoading stands in for the helper's lists (subscriptions,
// layers, history, kernels) until the first load answered: nothing for
// busyDelay, then rows skeleton rows at the real row height. It reports
// whether it drew in place of the list; draw the list or its empty state
// only when it returns false, so "还没有…" never shows before we know.
func (a *app) helperListsLoading(c *ui.Context, rows int) bool {
	d := &a.hd
	if d.loaded {
		return false
	}
	start := d.loadStart
	if start.IsZero() {
		start = time.Now()
		d.loadStart = start
	}
	if wait := time.Until(start.Add(busyDelay)); wait > 0 {
		time.AfterFunc(wait+10*time.Millisecond, func() { mygo.RunOnMain(a.invalidate) })
		return true
	}
	skeletonRows(c, rows)
	return true
}

// skeletonRows draws static placeholder rows (no shimmer: macOS lists
// don't): a title line about 40 % wide over a meta line about 60 %, at
// listRow's height.
func skeletonRows(c *ui.Context, rows int) {
	t := c.Theme()
	fill := pal(t).code
	bar := func(h float32, part float32) {
		ui.Row(c).Children(func() {
			ui.Box(c).Height(h).Radius(4).Background(fill).Basis(0).Grow(part)
			ui.Box(c).Basis(0).Grow(1 - part)
		})
	}
	// The rows are the list's own children, so a listBox draws its
	// dividers between them as between real rows.
	for i := range rows {
		r := listRow(c.Key(i)).Busy(true)
		if i == 0 {
			r.Role(ui.RoleStatus).Label(i18n.T.Loading)
		}
		r.Children(func() {
			bar(10, 0.4-float32(i%2)*0.08)
			bar(8, 0.6+float32(i%3)*0.06)
		})
	}
}

// loadingBox keeps the space of content that is loading (an editor) and,
// after busyDelay, draws a skeleton of it there (kubai R-23): a gutter and
// three code bars, static (no shimmer, as macOS), so the page doesn't jump
// when the content arrives. key names the content; a box not drawn for
// 2 s starts its delay again (busySpan's timing).
func (a *app) loadingBox(c *ui.Context, key string, height float32) ui.Element {
	t := c.Theme()
	now := time.Now()
	if a.loadingSeen == nil {
		a.loadingSeen = map[string][2]time.Time{}
	}
	ts, ok := a.loadingSeen[key]
	if !ok || now.Sub(ts[1]) > 2*time.Second {
		ts[0] = now
		time.AfterFunc(busyDelay+10*time.Millisecond, func() { mygo.RunOnMain(a.invalidate) })
	}
	ts[1] = now
	a.loadingSeen[key] = ts
	show := busySpan{start: ts[0]}.visible(now)
	fill := pal(t).code
	box := ui.Row(c.Key("loading-"+key)).Height(height).Radius(plat.innerRadius).Border(1, pal(t).groupLine).Clip().
		AlignItems(ui.Stretch).Busy(true).Role(ui.RoleStatus).Label(i18n.T.Loading)
	return box.Children(func() {
		if !show {
			return
		}
		ui.Box(c).Width(36).Background(fill).Shrink(0).AccessHidden(true) // the gutter
		ui.Column(c).Grow(1).Gap(10).Padding(12, 14).AccessHidden(true).Children(func() {
			for _, part := range []float32{0.6, 0.45, 0.7} {
				ui.Row(c).Children(func() {
					ui.Box(c).Height(8).Radius(4).Background(fill).Basis(0).Grow(part)
					ui.Box(c).Basis(0).Grow(1 - part)
				})
			}
		})
	})
}

// --- written but not applied ---

// unappliedNote is the 预览与应用 heading's mark while the composed config
// differs from the one mihomo runs (a.hd.unapplied): a dot and 有未应用的改动.
func (a *app) unappliedNote(c *ui.Context) {
	if !a.hd.unapplied {
		return
	}
	t := c.Theme()
	ui.Row(c.Key("unapplied")).Gap(6).AlignItems(ui.Center).Tooltip(i18n.T.UnappliedHint).Children(func() {
		dot(c, t.Accent, 6)
		small(c, i18n.T.Unapplied)
	})
}
