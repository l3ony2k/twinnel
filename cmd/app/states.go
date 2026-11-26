package main

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
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
// reached: what didn't load, why in plain words, the raw error small and
// selectable, then 重试 and 打开设置. It draws the whole box.
func (a *app) unreachableState(c *ui.Context, svc netService, err string) {
	t := c.Theme()
	T := i18n.T
	title, body, _ := svc.texts()
	groupBox(c, t).Children(func() {
		ui.Column(c).Gap(6).AlignItems(ui.Center).Padding(28, 16).Children(func() {
			ui.Icon(c, iconError).FontSize(iconLarge).TextColor(t.TextMuted)
			ui.Text(c, title).FontWeight(500)
			ui.Text(c, body).FontSize(plat.calloutSize).TextColor(t.TextMuted).TextAlign(ui.Center).Wrap().MaxWidth(420)
			if err != "" {
				ui.Text(c, err).Font("monospace").FontSize(plat.smallSize).TextColor(t.TextMuted).
					TextAlign(ui.Center).Wrap().MaxWidth(420).Selectable().Label(T.UnreachableDetail + "：" + err)
			}
			ui.Row(c).Gap(8).Margin(8, 0, 0, 0).Children(func() {
				a.retryButton(c.Key("retry-" + title))
				if pushButton(c.Key("open-settings"), T.OpenSettings).Clicked() {
					a.openPage(pageSettings)
				}
			})
		})
	})
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
		if spin {
			spinnerIcon(c)
		} else {
			ui.Icon(c, iconRefresh).FontSize(iconInline)
		}
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

// loadingBox keeps the space of content that is loading (an editor) with a
// spinner centred in it after busyDelay, so the page doesn't jump when the
// content arrives. key names the content; a box not drawn for 2 s starts
// its delay again.
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
	show := now.Sub(ts[0]) >= busyDelay
	return ui.Column(c.Key("loading-"+key)).Height(height).Radius(plat.controlRadius+2).Border(1, pal(t).groupLine).
		AlignItems(ui.Center).Justify(ui.Center).Busy(true).Children(func() {
		if show {
			ui.Spinner(c).Label(i18n.T.Loading)
		}
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
