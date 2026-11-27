package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/status"
)

const (
	pageOverview  = "overview"
	pageProxies   = "proxies"
	pageTailscale = "tailscale"
	pageHealth    = "health"
	pageLogs      = "logs"
	pageConfig    = "config"
	pageHistory   = "history"
	pageKernel    = "kernel"
	pageTakeover  = "takeover"
	pageSettings  = "settings"
	pageAbout     = "about"
	// pageInstall has no sidebar row: 关于 → 入门 opens it, and the
	// sidebar keeps 关于 selected while it shows (navParent).
	pageInstall = "install"
)

func (a *app) mainView(c *ui.Context) {
	a.applyTheme(c)
	t := c.Theme()
	if c.Shortcut(ui.Cmd, ui.KeyW) {
		a.win.Close()
		return
	}
	a.flushToast(c)
	if back := pageBack(a.page); back != "" { // ⌘[ as in Finder and System Settings
		c.OnWindowShortcut(ui.Cmd, ui.KeyBracketLeft, func() { a.showPage(back) })
	}
	s := a.mon.Snapshot()
	if a.shotSnap != nil { // shot mode: synthetic state over the fake one
		a.shotSnap(&s)
	}
	helperOutdatedNow = helperOutdated(s)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebarFrame(c, func() { a.sidebar(c, s) }) // sidebar.go
		content := ui.Column(c).Grow(1).Background(t.Background)
		if c.Vibrancy() && a.contentHairline() {
			content.BorderWidth(0, 0, 0, 1).BorderColor(pal(t).separator)
		}
		barH := toolbarHeight(c)
		pageBar = toolbarSpec{} // the page fills it in (pageHeader)
		content.Children(func() {
			scrolled := false
			switch a.page {
			case pageLogs:
				a.logsPage(c, barH) // has its own scrolling list, below the bar
			default:
				st := a.pageScroll[a.page]
				if st == nil {
					st = &ui.ScrollState{}
					a.pageScroll[a.page] = st
				}
				ui.Scroll(c.Key("page:"+a.page)).TrackScroll(st).Grow(1).ScrollbarInsets(barH, 0, 0, 0).Children(func() {
					// Left-anchored as System Settings' detail pane: every
					// page, 日志 included, starts on one edge; the column
					// stops growing at pageMaxWidth.
					ui.Column(c).Padding(barH+4, plat.pagePadX, 32).AlignItems(ui.Start).Children(func() {
						ui.Column(c).Gap(gapGroup).FillWidth().MaxWidth(pageMaxWidth(a.page)).Children(func() {
							a.pageBody(c, s)
						})
					})
				})
				scrolled = st.Y > 0.5
			}
			a.toolbar(c, barH, scrolled)
		})
	})
	a.sidebarChrome(c, toolbarHeight(c)) // the sidebar toggle and the divider's grip
	a.toasts(c)                          // toast.go
}

// The toolbar: the page's title and actions on the traffic lights' line,
// pinned while the page scrolls under it, as a unified toolbar on macOS
// (System Settings, Finder). At the top of the page it has no background;
// once content scrolls under it, the scroll edge of macOS 26 (content
// frosted under the window's background, a hairline below) sets it off.

type toolbarSpec struct {
	title    string
	trailing func()
	back     func() // a sub-page's way back: a ‹ button before the title (and ⌘[)
}

// pageBar is what the current page puts in the toolbar (pageHeader);
// mainView builds the toolbar after the page. Main thread only.
var pageBar toolbarSpec

// toolbarHeight is the toolbar's height: a band centered on the traffic
// lights' line, as a unified toolbar's (52 on macOS 26). The window's
// reported title bar height (66 under the hidden-inset style) is not that
// line (sidebar.go, trafficLightsMid).
func toolbarHeight(c *ui.Context) float32 {
	return max(2*trafficLightsMid(c), 52)
}

func (a *app) toolbar(c *ui.Context, h float32, scrolled bool) {
	t := c.Theme()
	if scrolled { // fades in and out as macOS 26's scroll edge does
		ui.Box(c.Key("toolbar-edge")).Absolute().Top(0).Left(0).Right(0).Height(h).PassThrough().
			Material(glass.ScrollEdge{Hard: true, Background: t.Background}).Transition(fadeMotion)
	}
	spec := pageBar
	// The title and the actions line up with the page's column: over its
	// left and right edges at every width, as the column is left-anchored
	// and no wider than pageMaxWidth. Logs fill the pane, and so does their
	// bar.
	inner := pageMaxWidth(a.page)
	if a.page == pageLogs {
		inner = 0
	}
	ui.Column(c.Key("toolbar")).Absolute().Top(0).Left(0).Right(0).Height(h).Padding(0, plat.pagePadX, 0, a.toolbarLead(c)).
		AlignItems(ui.Start).Justify(ui.Center).DragWindow().Role(ui.RoleToolbar).Label(spec.title).Children(func() {
		row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
		if inner > 0 {
			row.MaxWidth(inner)
		}
		row.Children(func() {
			if spec.back != nil {
				back := spec.back
				toolbarActions(c, func() {
					b := pushButton(c.Key("back"), "").Label(i18n.T.NavBack).Tooltip(i18n.T.NavBack)
					b.Children(func() {
						ui.Icon(c, iconCollapsed).FontSize(iconInline).Rotate(180)
					})
					if b.Clicked() {
						back()
					}
				})
			}
			// The title is the one thing the bar never drops: the page's
			// actions give way first (a page with many fits them itself).
			ui.Text(c, spec.title).FontSize(plat.titleBarSize).FontWeight(plat.titleBarWeight).SingleLine().Shrink(0)
			ui.Spacer(c)
			if spec.trailing != nil {
				toolbarActions(c, spec.trailing)
			}
		})
	})
}

// inToolbar is true while the toolbar's actions are built: buttons and
// segmented controls there take the toolbar's capsule (toolbarFace), not
// a push button's face. toolbarGlass is the glass they are panes of.
// Main thread only.
var (
	inToolbar    bool
	toolbarGlass *glass.Group
)

// toolbarActions builds a page's toolbar actions. Under Liquid Glass
// (macOS 26+) each button and segmented control is its own glass capsule
// (toolbarFace), and labels and switches sit bare between them, all on
// one line; elsewhere, the controls as built.
func toolbarActions(c *ui.Context, trailing func()) {
	if !liquidGlass {
		trailing()
		return
	}
	g := glass.NewGroup(c)
	g.Radius = toolbarItemH / 2
	g.Merge = 0 // separate capsules, each exactly its control's box
	inToolbar, toolbarGlass = true, g
	defer func() { inToolbar, toolbarGlass = false, nil }()
	g.Shrink(0).AlignItems(ui.Center).Children(func() {
		ui.Toolbar(c, trailing).Height(toolbarItemH).Padding(0).Gap(toolbarGap).AlignItems(ui.Center).Shrink(0)
	})
}

func (a *app) pageBody(c *ui.Context, s status.Snapshot) {
	switch a.page {
	case pageOverview:
		a.overviewPage(c, s)
	case pageProxies:
		a.proxiesPage(c, s)
	case pageTailscale:
		a.tailscalePage(c, s)
	case pageHealth:
		a.healthPage(c, s)
	case pageSettings:
		a.settingsPage(c, s)
	case pageConfig:
		a.configPage(c, s)
	case pageHistory:
		a.historyPage(c, s)
	case pageKernel:
		a.kernelPage(c, s)
	case pageTakeover:
		a.takeoverPage(c, s)
	case pageAbout:
		a.aboutPage(c, s)
	case pageInstall:
		a.installPage(c, s)
	case pageLeave:
		a.leavePage(c, s)
	}
}

// sidebar is the window's source list over the platform's sidebar
// material, with the duet pair's state at its foot.
func (a *app) sidebar(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	side := ui.Column(c).Width(a.sideInnerW()).Shrink(0).DragWindow()
	if !c.Vibrancy() { // an opaque stand-in for the material
		side.Background(ui.Hex("#ebebeb"))
		if t.Dark {
			side.Background(ui.Hex("#2a2a2c"))
		}
	}
	side.Padding(sidebarTop(c), 10, 12).Gap(4).Children(func() {
		old := a.page
		ts := 0
		if s.Tailscale.Reachable {
			ts = s.Tailscale.State.OnlinePeers()
		}
		// The one health number (status.Snapshot.HealthBadge): failures
		// in red when there are some, else warnings in amber; before
		// takeover, failures are findings, in amber.
		health, hl := s.HealthBadge()
		healthTone := toneAttention
		if hl == status.LevelError {
			healthTone = toneAlert
		}
		sections := navSections(ts, health, healthTone)
		a.pageKeys(c, sections)
		active := c.WindowActive()
		if a.shotNav.active != nil {
			active = *a.shotNav.active
		}
		ui.Scroll(c).Grow(1).Children(func() {
			// A sub-page keeps its parent's row selected; a click on that
			// row goes back to it.
			sel := navParent(a.page)
			if navList(c, &sel, sections, active, a.shotNav.hover, sel != a.page) {
				a.page = sel
				a.pageChanged(old)
			}
		})
		a.sideFooter(c, s) // the duet pair, or its marks on the rail (sidebar.go)
	})
}

// navSections are the sidebar's pages in order, with the counts their
// rows show (Tailscale devices online, health problems). The first nine
// pages get ⌘1…⌘9 (numberPages); 设置 has ⌘, as in every Mac app; 关于,
// the tenth, has none (the app menu's 关于 duet opens it).
func navSections(ts, health int, healthTone navTone) []navSection {
	T := i18n.T
	sections := []navSection{
		{items: []navItem{
			{id: pageOverview, icon: iconOverview, label: T.PageOverview},
			{id: pageProxies, icon: iconProxies, label: T.PageProxies},
			{id: pageTailscale, icon: iconTailscale, label: T.PageTailscale, count: ts},
			{id: pageHealth, icon: iconHealth, label: T.PageHealth, count: health, tone: healthTone},
		}},
		{title: T.SidebarHelper, items: []navItem{
			{id: pageConfig, icon: iconConfig, label: T.NavConfig},
			{id: pageHistory, icon: iconHistory, label: T.PageHistory},
			{id: pageKernel, icon: iconKernel, label: T.PageKernel},
			{id: pageTakeover, icon: iconTakeover, label: T.PageTakeover},
		}},
		{title: T.SidebarApp, items: []navItem{
			{id: pageLogs, icon: iconLogs, label: T.PageLogs},
			{id: pageSettings, icon: iconSettings, label: T.PageSettings},
			{id: pageAbout, icon: iconAbout, label: T.PageAbout},
		}},
	}
	n := 0
	for si := range sections {
		for ii := range sections[si].items {
			it := &sections[si].items[ii]
			if it.id == pageSettings || n == len(digitKeys) {
				continue
			}
			it.key, it.digit = digitKeys[n], n+1
			n++
		}
	}
	return sections
}

// digitKeys are the keys of ⌘1…⌘9, in order.
var digitKeys = []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}

// pageKeys registers ⌘1…⌘9 for the pages that have one, on every page
// (the sidebar is built with each of them).
func (a *app) pageKeys(c *ui.Context, sections []navSection) {
	for _, sec := range sections {
		for _, it := range sec.items {
			if it.digit == 0 {
				continue
			}
			id := it.id
			c.OnWindowShortcut(ui.Cmd, it.key, func() { a.showPage(id) })
		}
	}
}

// showPage turns the main window to a page, as a click on its sidebar row
// does. Main thread.
func (a *app) showPage(id string) {
	if a.page == id {
		return
	}
	old := a.page
	a.page = id
	a.pageChanged(old)
	if a.win != nil {
		a.win.Invalidate()
	}
}

func (a *app) pageChanged(old string) {
	noteSubPage(a.page, old)
	if a.page != old && a.hd.busy == "" {
		a.hd.msg, a.hd.msgErr = "", false // the last outcome belongs to the page it ran on
	}
	if a.page == pageLogs {
		a.mon.SetLogLevel(a.logs.levelName())
	} else if old == pageLogs {
		a.mon.SetLogLevel("")
	}
	if a.page == pageSettings {
		a.settings.load(a.cfg)
	}
	if a.page == pageAbout {
		a.aboutOpened()
	}
	switch a.page {
	case pageTakeover, pageAbout, pageKernel:
		a.detectLiveSetup() // again on each visit: the service may have changed
	}
	if a.page == pageHealth && old != pageHealth {
		a.runHealth()
	}
	switch a.page {
	case pageConfig, pageHistory:
		a.loadHelperData()
	case pageKernel:
		a.loadHelperData()
		if a.hd.kern.ts == nil && a.hd.connected {
			a.tailscaleCheck()
		}
		a.loadServices()
	case pageTakeover:
		a.loadTakeover()
	}
	a.syncHealthVisible()
}

func pageTitle(c *ui.Context, title string) {
	pageHeader(c, title, nil)
}

// --- overview (overview.go) ---

// verdict is the overall state in one line, with what makes it so.
func (a *app) verdict(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	icon := iconOK
	switch s.Level {
	case status.LevelWarn:
		icon = iconWarning
	case status.LevelError:
		icon = iconError
	case status.LevelUnknown:
		icon = iconSkip
	}
	groupBox(c, t).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, icon).FontSize(iconLarge).TextColor(levelColor(t, s.Level)).Shrink(0)
			ui.Column(c).Gap(4).Grow(1).Children(func() {
				head, reasons := levelHeadline(s)
				ui.Text(c, head).FontSize(plat.leadSize).FontWeight(600)
				for _, r := range reasons {
					ui.Text(c, r.Text).TextColor(t.TextMuted).Wrap().LineHeight(lineText).Selectable()
					if r.Raw != "" {
						// The system's own words, secondary (as errorNote).
						ui.Text(c, r.Raw).Font("monospace").FontSize(plat.smallSize).TextColor(t.TextMuted).Wrap().
							LineHeight(lineText).Selectable().Label(i18n.T.UnreachableDetail + "：" + r.Raw)
					}
				}
				a.verdictActions(c, s)
			})
		})
	})
}

func sourceSuffix(s string) string {
	if s == "" {
		return ""
	}
	return " · " + s
}

func onOff(b bool) string {
	if b {
		return i18n.T.On
	}
	return i18n.T.Off
}

// trafficSpan names how much time the chart shows: 最近 45 秒, 最近 2 分钟.
func trafficSpan(d time.Duration) string {
	sec := int(d.Round(time.Second) / time.Second)
	if sec < 60 {
		return i18n.F(i18n.T.TrafficLastSec, max(sec, 1))
	}
	return i18n.F(i18n.T.TrafficLastMin, sec/60)
}

// trafficChart draws the last two minutes of download and upload rates:
// the overview's one rich element. Download carries the accent; upload is
// a quiet second line.
func (a *app) trafficChart(c *ui.Context, s status.Snapshot, height float32) {
	t := c.Theme()
	T := i18n.T
	hist := a.mon.TrafficHistory()
	up := t.TextMuted
	// Room under the figures: the plot's top tick label sits at its top
	// edge and would touch the 累计 line otherwise.
	ui.Row(c).Gap(28).AlignItems(ui.End).Margin(0, 0, 8, 0).Children(func() {
		figure := func(label string, col ui.Color, rate int64) {
			ui.Column(c).Gap(2).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(10, 2).Radius(1).Background(col)
					small(c, label)
				})
				num(ui.Text(c, status.Rate(rate)).FontSize(plat.figureSize).FontWeight(500))
			})
		}
		figure(T.Download, t.Accent, s.Mihomo.Traffic.Down)
		figure(T.Upload, up, s.Mihomo.Traffic.Up)
	})
	const n = 120
	var hi float64 = 1024
	for _, h := range hist {
		hi = max(hi, float64(h.Down), float64(h.Up))
	}
	// The scale is built in the unit the labels use, so its ticks are
	// round there (200 / 400 / 600 KB/s, not 195.3 / 390.6 / 585.9).
	unit, unitName := rateUnit(hi)
	y := chart.NewLinear(0, hi/unit).Nice(3)
	ticks := y.Ticks(3)
	for i := range ticks {
		ticks[i].Label = strconv.FormatFloat(ticks[i].Value, 'f', -1, 64) + " " + unitName
	}
	xs := make([]float32, len(hist))
	down := make([]float32, len(hist))
	upv := make([]float32, len(hist))
	// The time axis spans the samples there are, so the graph fills the
	// width from the first seconds and widens to the n-sample window as
	// samples come in; the caption above names the span.
	span := min(max(len(hist), 2), n)
	for i, h := range hist {
		xs[i] = float32(span-len(hist)+i) / float32(span-1)
		down[i] = y.At(float64(h.Down) / unit)
		upv[i] = y.At(float64(h.Up) / unit)
	}
	grid := t.Text.Alpha(0.06) // quieter than a separator: the plot's lines lead (R-unslop-9)
	axes := chart.Axes{Right: ticks, Label: func(_ chart.Side, tk chart.Tick) {
		monoText(c, tk.Label).FontSize(plat.smallSize).TextColor(t.TextMuted).NoWrap()
	}}
	describe := func(i int) string {
		h := hist[i]
		return fmt.Sprintf("%s %s · %s %s · %s", T.Download, status.Rate(h.Down), T.Upload, status.Rate(h.Up), status.Ago(s.Time, h.Time))
	}
	chart.Plot(c, axes, func(area ui.Element) {
		area.Draw(func(p *ui.Painter, r ui.Rect) {
			for _, tk := range ticks {
				p.Line(r.X, chart.Y(r, tk.At), r.X+r.W, chart.Y(r, tk.At), 1, grid)
			}
			if len(xs) < 2 {
				return
			}
			// Download's area is a tone ramp in the accent, strongest at
			// the line's highest point and gone at the baseline, so the
			// line leads and the fill never reads as a slab (R-unslop-9).
			var peak float32
			for _, v := range down {
				peak = max(peak, v)
			}
			ramp := r
			ramp.Y = chart.Y(r, peak)
			ramp.H = r.Y + r.H - ramp.Y
			p.ClipPath(chart.Area(r, xs, nil, down, chart.CurveMonotone), func() {
				p.FillGradient(ramp, ui.LinearGradient{From: t.Accent.Alpha(trafficRampAlpha(t)), To: t.Accent.Alpha(0), Angle: 180}, 0)
			})
			p.StrokePath(chart.Line(r, xs, upv, chart.CurveMonotone), 1.25, up)
			p.StrokePath(chart.Line(r, xs, down, chart.CurveMonotone), 1.5, t.Accent)
		})
		// The readout: under the pointer (or the arrow keys), a guide at
		// the nearest sample, a dot on each line, and the sample's rates
		// beside it, flipped to the guide's left near the right edge.
		sel := ui.Local(area, "duet.traffic.sel", func() int { return -1 })
		if sel == nil || len(xs) < 2 {
			return
		}
		if shotReadout {
			*sel = len(xs) * 2 / 3
		}
		chart.Select(c, area, sel, xs, describe)
		i := *sel
		if i < 0 || i >= len(xs) {
			return
		}
		chart.RuleX(c, xs[i], 1).Background(t.Text.Alpha(0.25)).PassThrough()
		chart.Point(c, xs[i], upv[i], 6, 6).Radius(3).Background(up).PassThrough()
		chart.Point(c, xs[i], down[i], 6, 6).Radius(3).Background(t.Accent).PassThrough()
		side := ui.Start
		if xs[i] > 0.6 {
			side = ui.End
		}
		chart.Anchor(c, xs[i], 1, side, ui.Start, func() {
			lbl := ui.Column(c).Gap(2).Padding(6, 8).Radius(plat.controlRadius).
				Background(pal(t).group).Border(1, pal(t).groupLine).
				Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.12)).PassThrough().Shrink(0)
			if side == ui.Start {
				lbl.MarginStart(8)
			} else {
				lbl.MarginEnd(8)
			}
			lbl.Children(func() {
				h := hist[i]
				readout := func(col ui.Color, label string, rate int64) {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(8, 2).Radius(1).Background(col)
						small(c, label).NoWrap()
						num(ui.Text(c, status.Rate(rate)).FontSize(plat.calloutSize).NoWrap())
					})
				}
				readout(t.Accent, T.Download, h.Down)
				readout(up, T.Upload, h.Up)
				num(small(c, status.Ago(s.Time, h.Time)).NoWrap())
			})
		})
	}).Label(T.Traffic).Height(height).Gap(8)
}

// trafficRampAlpha is the download fill's opacity at the line: lighter
// in the light appearance, where the same alpha reads stronger.
func trafficRampAlpha(t *ui.Theme) float32 {
	if t.Dark {
		return 0.22
	}
	return 0.16
}

// shotReadout shows the chart's readout in screenshots (DUET_SHOTS_READOUT),
// where no pointer is over it.
var shotReadout = os.Getenv("DUET_SHOTS") != "" && os.Getenv("DUET_SHOTS_READOUT") != ""

// rateUnit picks the unit a chart of rates up to hi bytes/s labels its
// ticks in: KB/s below 1 MB/s, else MB/s (or GB/s).
func rateUnit(hi float64) (float64, string) {
	switch {
	case hi >= 1<<30:
		return 1 << 30, "GB/s"
	case hi >= 1<<20:
		return 1 << 20, "MB/s"
	}
	return 1 << 10, "KB/s"
}

// --- proxies ---

func (a *app) proxiesPage(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	m := s.Mihomo
	pageHeader(c, T.PageProxies, func() {
		if !m.Reachable || len(m.Groups) == 0 {
			return
		}
		// One batch tests every node once (status.Monitor.TestAll); the
		// progress shows beside the button, which keeps its label and
		// width; its icon turns into a spinner while the batch runs.
		done, total, busy := a.mon.DelayProgress()
		if busy && total > 0 {
			num(small(c, i18n.F(T.TestAllProgress, done, total))).SingleLine()
		}
		b := pushButton(c.Key("test-all"), "").Label(T.TestAllDelay).Disabled(busy)
		spin := showBusy(c, b, busy)
		b.Children(func() {
			busyIcon(c, iconSpeed, spin)
			ui.Text(c, T.TestAllDelay).SingleLine()
		})
		if b.Clicked() {
			mon := a.mon
			go func() {
				// The tests are bound to the monitor: they stop when the
				// app quits; this only bounds the wait.
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				_ = mon.TestAll(ctx)
			}()
		}
	})
	if !m.Reachable {
		a.unreachableState(c, svcMihomo, m.Err)
		return
	}
	a.actionNotes(c, s)
	if len(m.Groups) == 0 {
		groupBox(c, t).Children(func() { emptyState(c, iconProxies, T.NoGroups, T.NoGroupsHint) })
		return
	}
	if len(a.groupsOpen) == 0 && m.Primary != nil {
		a.groupsOpen[m.Primary.Name] = true
	}
	// One list: a row per group, its nodes under it while open.
	listBox(c, t).Children(func() {
		for _, g := range m.Groups {
			a.groupRow(c.Key("group:"+g.Name), g, s.Can().Mihomo)
		}
	})
}

// Node tiles: a grid whose columns share the row's width evenly, each
// tile between tileMin and tileMax wide (the owner's 220–320), so every
// full row spans the width with equal gaps. A group with fewer nodes than
// columns gets one column per node, its grid no wider than tiles of
// tileMax: no empty columns at the right.
const (
	tileMin = 220
	tileMax = 320
	tileGap = 8
)

// tileLayout is the tile grid for n nodes in a row w wide: its columns,
// and the widest the grid may be (0: the whole row). Before the first
// layout (w 0) it guesses 3 columns.
func tileLayout(w float32, n int) (cols int, maxW float32) {
	if w <= 0 {
		return min(3, max(n, 1)), 0
	}
	fit := max(1, int((w+tileGap)/(tileMin+tileGap)))                      // most columns of at least tileMin
	need := max(1, int(math.Ceil(float64((w+tileGap)/(tileMax+tileGap))))) // fewest of at most tileMax
	cols = min(need, fit)
	if n < cols {
		return max(n, 1), float32(n)*tileMax + float32(n-1)*tileGap
	}
	return cols, 0
}

func (a *app) groupRow(c *ui.Context, g status.Group, writes bool) {
	t := c.Theme()
	T := i18n.T
	open := a.groupsOpen[g.Name]
	sel := pendingValue(a, nodeKey(g.Name), g.Now) // the pick shows at once
	chain := strings.Join(g.Chain, " → ")
	ui.Column(c).Gap(8).PaddingY(listRowPadY).Clip().Transition(expandFor(open)).Children(func() {
		// The head is a disclosure the keyboard reaches: Tab stops on it,
		// Return or Space opens and closes the group.
		head := ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(28).
			Role(ui.RoleDisclosure).Label(g.Name).Expanded(open).Focusable().FocusRingOffset(2)
		head.ContextMenu(func(m *ui.Menu) {
			label := T.GroupExpand
			if open {
				label = T.GroupCollapse
			}
			if m.Item(label).Chosen() {
				a.groupsOpen[g.Name] = !open
			}
			m.Separator()
			if m.Item(T.TestGroupDelay).Disabled(a.mon.Testing(g.Name)).Chosen() {
				a.testGroup(g.Name)
			}
			if m.Item(T.CopyName).Chosen() {
				copyText(c, g.Name)
			}
		})
		head.Children(func() {
			disclosure(c, open)
			ui.Text(c, g.Name).FontWeight(500).SingleLine().Shrink(0) // a row title, not a heading (R-unslop-30)
			typeMeta(c, g.Type)
			ui.Text(c, chain).TextColor(t.TextMuted).SingleLine().Shrink(1).Tooltip(chain)
			ui.Spacer(c)
			num(ui.Text(c, g.DelayText()).TextColor(delayColor(t, g.Delay, g.Tested)).Shrink(0))
			testing := a.mon.Testing(g.Name)
			if toolIcon(c.Key("test"), iconSpeed, T.TestGroupDelay, testing).Clicked() && !testing {
				a.testGroup(g.Name)
			}
		})
		if head.Clicked() {
			a.groupsOpen[g.Name] = !open
		}
		a.controlError(c, nodeKey(g.Name))
		if !open {
			return
		}
		// The head row spans the list's width; the grid is 22 in from it,
		// under the name.
		cols, maxW := tileLayout(head.Bounds().W-22, len(g.Members))
		if cols != a.tileCols[g.Name] { // the width changed: lay out again with the new count
			a.tileCols[g.Name] = cols
			c.Invalidate()
		}
		// The tiles are one stop of Tab (entering at the selected node),
		// ← and → step through them, ↑ and ↓ by a row of the grid
		// (nodeTile). The arrows only move the focus: Return or Space
		// selects, as switching the node changes where traffic goes.
		grid := ui.Grid(c.Key("tiles")).Gap(tileGap).MarginStart(22).MarginY(2).Columns(cols).
			Role(ui.RoleRadioGroup).Label(g.Name).FocusGroup(ui.Horizontal)
		if maxW > 0 {
			grid.MaxWidth(maxW)
		}
		grid.Children(func() {
			for i, mem := range g.Members {
				a.nodeTile(c.Key(mem.Name), g, i, cols, mem, mem.Name == sel, writes)
			}
		})
	})
}

// nodeTile is one node of an open group: its name, type and delay. The
// selected one has the accent's soft fill and a quiet accent hairline;
// only colors animate, quickly, so tiles never slide on a resize. i is its
// place in the group, cols the grid's columns (for ↑ and ↓).
func (a *app) nodeTile(c *ui.Context, g status.Group, i, cols int, mem status.Member, selected, writes bool) {
	t := c.Theme()
	p := pal(t)
	T := i18n.T
	typ := mem.Type
	if typ == "" {
		typ = "?"
	}
	delay := mem.DelayText()
	desc := mem.Name + " · " + typ + " · " + delay
	canSelect := writes && g.Selectable() && !selected
	// Every tile takes the focus, so the keyboard and VoiceOver can read
	// the whole group; one that cannot be picked (read-only mode, or a
	// group mihomo picks for itself) says so as unavailable.
	// A tile is one step up from its card: in dark a faint white over
	// the group fill, not the darker code fill that read as holes
	// (R-unslop-16); in light the code fill already steps.
	fill := p.code
	if t.Dark {
		fill = ui.RGBA(255, 255, 255, 0.04)
	}
	chip := ui.Column(c).MinWidth(0).Padding(8, 10).Gap(2).Radius(plat.innerRadius).
		Background(fill).Border(1, p.groupLine).Transition(colorMotion).
		Role(ui.RoleRadio).Label(desc).AccessSelected(selected).Tooltip(desc).
		Focusable().AccessDisabled(!canSelect && !selected)
	if selected {
		chip.Background(p.accentSoft).Border(1, t.Accent.Alpha(0.35))
	}
	if canSelect {
		if chip.Hovered() {
			chip.Background(t.SurfaceHover)
		}
	}
	if f := a.tileFocus; f.group == g.Name && f.node == mem.Name {
		chip.Focus()
		a.tileFocus = tileRef{}
	}
	if j, ok := tileStep(i, cols, len(g.Members), chip.Shortcut(0, ui.KeyUp), chip.Shortcut(0, ui.KeyDown)); ok {
		a.tileFocus = tileRef{g.Name, g.Members[j].Name}
		c.Invalidate() // a tile above is built already
	}
	chip.ContextMenu(func(m *ui.Menu) {
		if m.Item(T.NodeUse).Disabled(!canSelect).Chosen() {
			a.selectNode(g.Name, mem.Name)
		}
		m.Separator()
		if m.Item(T.TestNodeDelay).Disabled(mem.Builtin).Chosen() {
			a.testNode(mem.Name)
		}
		if m.Item(T.CopyNodeName).Chosen() {
			copyText(c, mem.Name)
		}
	})
	chip.Children(func() {
		ui.Text(c, mem.Name).SingleLine()
		ui.Row(c).Gap(6).Children(func() {
			typeMeta(c, typ).Grow(1).Shrink(1)
			num(ui.Text(c, delay).FontSize(plat.smallSize).TextColor(delayColor(t, mem.Delay, mem.Tested)))
		})
	})
	if chip.Clicked() && canSelect {
		a.selectNode(g.Name, mem.Name)
	}
}

// typeMeta is a mihomo group or proxy type (Selector, URLTest, VLESS):
// an identifier, not a word to translate, so small, muted and mono
// (R-unslop-31), protocols in their own spelling.
func typeMeta(c *ui.Context, typ string) ui.Element {
	return mono(meta(c, protoName(typ)))
}

// protoName spells a proxy type as its protocol does; mihomo reports
// them capitalised (Vless, Tuic).
func protoName(typ string) string {
	switch strings.ToLower(typ) {
	case "vless":
		return "VLESS"
	case "vmess":
		return "VMess"
	case "tuic":
		return "TUIC"
	case "ssr":
		return "SSR"
	case "ssh":
		return "SSH"
	case "wireguard":
		return "WireGuard"
	case "anytls":
		return "AnyTLS"
	}
	return typ
}

// tileRef names a node tile that takes the focus when next built.
type tileRef struct{ group, node string }

// tileStep is where ↑ (up) or ↓ (down) moves the focus from tile i of n
// in a grid of cols columns: the tile a row above or below, and false at
// the grid's top or bottom row (the focus stays).
func tileStep(i, cols, n int, up, down bool) (int, bool) {
	cols = max(cols, 1)
	switch {
	case up && i-cols >= 0:
		return i - cols, true
	case down && i+cols < n:
		return i + cols, true
	}
	return 0, false
}

// testGroup delay-tests the nodes of a group, nested groups opened (reads:
// GET /proxies/<node>/delay, shared with tests already running), and
// opens the group to show the results.
func (a *app) testGroup(name string) {
	mon := a.mon
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = mon.TestGroup(ctx, name)
	}()
	a.groupsOpen[name] = true
}

// testNode delay-tests one node (GET /proxies/<name>/delay).
func (a *app) testNode(name string) {
	mon := a.mon
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = mon.TestNode(ctx, name)
	}()
}

// --- tailscale ---

func (a *app) tailscalePage(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	ts := s.Tailscale
	pageHeader(c, T.PageTailscale, func() {
		if ts.Reachable {
			small(c, T.TSState(ts.State.BackendState))
			a.tsPowerSwitch(c, s)
		}
	})
	if !ts.Reachable {
		a.unreachableState(c, svcTailscale, ts.Err)
		return
	}
	a.actionNotes(c, s)
	a.controlError(c, pendTS)
	st := ts.State
	card(c, "", func() {
		ui.Row(c).Gap(24).Children(func() {
			ui.Column(c).Gap(gapRow).Basis(0).Grow(1).Children(func() {
				kv(c, T.Account, st.User)
				kvMono(c, T.Version, st.Version)
				kv(c, T.Install, T.InstallKind(string(ts.Install.Kind))+sourceSuffix(ts.Install.Source))
			})
			ui.Column(c).Gap(gapRow).Basis(0).Grow(1).Children(func() {
				kvTip(c, T.MagicDNS, T.TipMagicDNS, onOff(st.Prefs.MagicDNS))
				kv(c, T.ShieldsUp, onOff(st.Prefs.ShieldsUp))
				kvTipWith(c, T.ExitNode, T.InfoExitNode, nil, func() { kvControl(a.exitNodePicker(c, s)) })
			})
		})
		a.controlError(c, pendExit)
	})
	peers := st.Peers
	if a.shotNoPeers { // shots of the empty devices card (shots_final.go)
		peers = nil
	}
	online := 0
	for _, p := range peers {
		if p.Online {
			online++
		}
	}
	title := fmt.Sprintf("%s · %d / %d", T.Devices, online, len(peers))
	if len(peers) == 0 { // the empty state says it (R-unslop-29)
		title = T.Devices
	}
	cardTip(c, title, T.TipPeerPath, nil, func() {
		if len(peers) == 0 {
			// In a card under the account's: the card size of an empty
			// state (one row, the title alone), not a page's.
			cardEmpty(c, iconTailscale, T.NoPeers)
			return
		}
		a.peers.RowHeight = 32
		// Each row's context menu copies the device's address or name.
		a.peers.CellMenu = func(m *ui.Menu, row, _ int) {
			if row < 0 || row >= len(peers) {
				return
			}
			p := peers[row]
			ip := p.IPv4()
			if m.Item(T.CopyIP).Disabled(!ip.IsValid()).Chosen() {
				copyText(c, ip.String())
			}
			if m.Item(T.CopyName).Chosen() {
				copyText(c, p.Name)
			}
		}
		// The name takes the room the others leave, so the table fits a
		// narrow window (900 px) and the last column is never cut; the
		// others are as wide as their longest value (windows, 中继 · tok)
		// in either language (R-unslop-3).
		cols := []ui.TableColumn{
			{Title: T.PeerName, MinWidth: 120},
			{Title: T.PeerIP, Width: 120},
			{Title: T.PeerOS, Width: 84},
			{Title: T.PeerPath, Width: 96},
			{Title: T.PeerRelay, Width: 56},
			{Title: T.PeerLastSeen, Width: 88},
		}
		ui.Table(c, &a.peers, cols, len(peers), func(row, col int) {
			p := peers[row]
			switch col {
			case 0:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					col := t.TextMuted.Alpha(0.6)
					if p.Online {
						col = t.Success
					}
					dot(c, col, 7)
					ui.Text(c, p.Name).SingleLine().Tooltip(p.Name)
				})
			case 1:
				ip := ""
				if v4 := p.IPv4(); v4.IsValid() {
					ip = v4.String()
				}
				monoText(c, ip).SingleLine().Selectable()
			case 2:
				ui.Text(c, p.OS).TextColor(t.TextMuted).SingleLine()
			case 3:
				// The status mark, not a glyph (R-unslop-18): an accent dot
				// for direct, a grey one for a relay (a fact of the
				// network, not a problem: no alarm color), none when idle.
				mark, col := ui.Color{}, t.TextMuted
				switch p.Path() {
				case "direct":
					mark, col = t.Accent, t.Text
				case "derp", "peer-relay":
					mark, col = t.TextMuted, t.Text
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if mark != (ui.Color{}) {
						dot(c, mark, 6).Shrink(0)
					}
					ui.Text(c, T.Path(p.Path())).TextColor(col).SingleLine().Tooltip(T.Path(p.Path()))
				})
			case 4:
				ui.Text(c, p.Relay).TextColor(t.TextMuted).SingleLine()
			case 5:
				seen := T.Online
				if !p.Online {
					seen = T.Offline
					if !p.LastSeen.IsZero() {
						seen = status.Ago(s.Time, p.LastSeen)
					}
				}
				ui.Text(c, seen).TextColor(t.TextMuted).SingleLine()
			}
		}).Height(float32(min(len(peers), 14)*32 + 36)).Background(ui.Transparent).MarginX(-8)
	})
}

// --- settings ---

type settingsForm struct {
	controller, secret, primary, testURL string
	msg                                  string
	failed                               bool
	mihomoErr                            bool // msg belongs to the mihomo card
}

// dirty reports whether the mihomo fields differ from the saved config.
func (f *settingsForm) dirty(c status.Config) bool {
	primary := strings.TrimSpace(f.primary)
	if primary == "" {
		primary = status.DefaultPrimaryGroup
	}
	cur := c.PrimaryGroup
	if cur == "" {
		cur = status.DefaultPrimaryGroup
	}
	if strings.TrimSpace(f.controller) != c.Controller || primary != cur {
		return true
	}
	return !c.SecretFromEnv && f.secret != c.Secret
}

func (f *settingsForm) load(c status.Config) {
	*f = settingsForm{
		controller: c.Controller, primary: c.PrimaryGroup, testURL: c.TestURL,
	}
	if !c.SecretFromEnv {
		f.secret = c.Secret
	}
}

func (a *app) settingsPage(c *ui.Context, s status.Snapshot) {
	pageTitle(c, i18n.T.PageSettings)
	// 通用 (login, notifications, language), 菜单栏, mihomo, 后台助手 and
	// 停止使用 duet. Every row is the label, then its control at the
	// card's right edge (R-unslop-5).
	a.generalCard(c)
	if plat.trayTitle {
		a.menuBarCard(c)
	}
	a.mihomoSettingsCard(c, s)
	a.refreshHelperMgmt(s)
	a.helperSection(c, s)
}

// settingsFieldW is the width of a 设置 text field or pop-up: an address
// or a group name, not the card's width.
const settingsFieldW = 260

// settingsRow is one row of a 设置 card: the label (with an ⓘ when tip
// is set) taking the room, the control at the right edge.
func settingsRow(c *ui.Context, label, tip string, more func(), control func()) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).MinHeight(28).Children(func() {
		switchLabel(c, label, tip, more)
		control()
	})
}

// generalCard is 设置 → 通用: 登录时打开, 出现故障时发通知 and the
// interface language, one card for three one-row settings.
func (a *app) generalCard(c *ui.Context) {
	T := i18n.T
	card(c, T.General, func() {
		a.loginRow(c)
		on := !a.cfg.NotificationsOff
		ok := notificationsSupported()
		settingsRow(c, T.NotificationsSwitch, T.TipNotifications, nil, func() {
			if tg := needsBundleTag(appBundled()); !ok && tg != "" {
				// Dev runs cannot post notifications: the switch is off
				// limits, and the tag says why (the tooltip, how).
				tag(c, tg).Tooltip(T.NotificationsUnsupportedTip)
			}
			if ui.Switch(c.Key("notify"), &on).Label(T.Notifications).Disabled(!ok).Changed() {
				a.setNotifications(on)
			}
		})
		a.languageRow(c)
		if f := &a.settings; f.msg != "" && f.failed && !f.mihomoErr {
			errorNote(c, f.msg)
		}
	})
}

// mihomoSettingsCard is 设置 → mihomo: the controller, its secret and the
// main group. They take effect together (the monitor reconnects), so
// 保存 is the card's one action, in its heading, enabled once something
// changed (R-unslop-6). While the helper provides the controller, its
// address and secret are values, not locked fields.
func (a *app) mihomoSettingsCard(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	t := c.Theme()
	f := &a.settings
	fromHelper := a.creds.cur != nil
	dirty := f.dirty(a.cfg)
	cardWith(c, T.Mihomo, func() {
		if ui.PrimaryButton(c.Key("save"), T.Save).Disabled(!dirty).Clicked() {
			a.saveSettings()
		}
	}, func() {
		field := func(key, label string, v *string, password bool) {
			settingsRow(c, label, "", nil, func() {
				in := ui.TextInput(c.Key(key), v).Label(label).Width(settingsFieldW)
				if password {
					in.Password()
				}
				in.OnSubmit(func() { // Return saves, as 保存
					if f.dirty(a.cfg) {
						a.saveSettings()
					}
				})
			})
		}
		value := func(label, v string, isMono bool) {
			settingsRow(c, label, "", nil, func() {
				e := ui.Text(c, v).TextColor(t.TextMuted).SingleLine().Selectable()
				if isMono {
					mono(e)
				}
			})
		}
		switch {
		case fromHelper:
			value(T.Controller, a.cfg.Controller, true)
			value(T.Secret, T.ControllerFromHelper, false)
		case a.cfg.SecretFromEnv:
			field("controller", T.Controller, &f.controller, false)
			value(T.Secret, T.SecretFromEnv, false)
		default:
			field("controller", T.Controller, &f.controller, false)
			field("secret", T.Secret, &f.secret, true)
		}
		// The main group by its own name, as mihomo lists it: a picker
		// of the groups one can choose a node in, while mihomo answers.
		settingsRow(c, T.PrimaryGroup, T.TipPrimaryGroup, nil, func() {
			if names := selectorGroups(s); len(names) > 0 {
				if !contains(names, f.primary) && f.primary != "" {
					names = append([]string{f.primary}, names...)
				}
				ui.Select(c.Key("primary"), &f.primary, names).Label(T.PrimaryGroup).Width(settingsFieldW)
			} else {
				ui.TextInput(c.Key("primary"), &f.primary).Label(T.PrimaryGroup).Width(settingsFieldW)
			}
		})
		// No mihomo 程序 / 配置目录 fields: nothing read them (the helper
		// keeps its own, found by takeover; walkthrough 2, N27).
		if f.msg != "" && f.failed && f.mihomoErr {
			errorNote(c, f.msg)
		}
	})
}

// settingsLabel is the label column of a 设置 field row, with an ⓘ after
// the label when tip is not empty.
func settingsLabel(c *ui.Context, label, tip string) ui.Element {
	t := c.Theme()
	if tip == "" {
		return ui.Text(c, label).TextColor(t.TextMuted).Width(labelWidth + 24).Shrink(0)
	}
	return ui.Row(c).Gap(4).AlignItems(ui.Center).Width(labelWidth + 24).Shrink(0).Children(func() {
		ui.Text(c, label).TextColor(t.TextMuted).SingleLine().Shrink(1)
		infoTipAbout(c, label, tip, nil)
	})
}

// switchLabel is the label of a 设置 switch row, taking the room before
// the switch, with an ⓘ after it.
func switchLabel(c *ui.Context, label, tip string, more func()) {
	ui.Row(c).Gap(4).AlignItems(ui.Center).Grow(1).Shrink(1).Children(func() {
		ui.Text(c, label).Shrink(1)
		infoTipAbout(c, label, tip, more)
	})
}

// selectorGroups are mihomo's groups a node can be chosen in, by name.
func selectorGroups(s status.Snapshot) []string {
	var out []string
	if !s.Mihomo.Reachable {
		return nil
	}
	for _, g := range s.Mihomo.Groups {
		if g.Selectable() && g.Name != "GLOBAL" {
			out = append(out, g.Name)
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (a *app) setNotifications(on bool) {
	cfg := a.cfg
	cfg.NotificationsOff = !on
	if err := status.SaveConfig(a.cfgPath, cfg); err != nil {
		a.settings.msg, a.settings.failed, a.settings.mihomoErr = i18n.T.SaveFailed+err.Error(), true, false
		return
	}
	a.cfg = cfg
}

// actionNotes shows the last action's error. Read-only mode is shown
// once, in the sidebar's state footer (duetPair).
func (a *app) actionNotes(c *ui.Context, s status.Snapshot) {
	if a.actionErr != "" {
		errorNote(c, a.actionErr)
	}
}

func (a *app) saveSettings() {
	f := &a.settings
	cfg := a.cfg
	cfg.Controller = strings.TrimSpace(f.controller)
	cfg.PrimaryGroup = strings.TrimSpace(f.primary)
	cfg.TestURL = strings.TrimSpace(f.testURL)
	if !cfg.SecretFromEnv {
		cfg.Secret = f.secret
	}
	if cfg.PrimaryGroup == "" {
		cfg.PrimaryGroup = status.DefaultPrimaryGroup
	}
	if _, err := mihomo.New(mihomo.Options{Address: cfg.Controller}); err != nil {
		f.msg, f.failed, f.mihomoErr = i18n.T.SaveFailed+err.Error(), true, true
		return
	}
	if err := status.SaveConfig(a.cfgPath, cfg); err != nil {
		f.msg, f.failed, f.mihomoErr = i18n.T.SaveFailed+err.Error(), true, true
		return
	}
	a.cfg = cfg
	f.msg, f.failed, f.mihomoErr = "", false, false
	a.toast = i18n.T.Saved
	a.groupsOpen = map[string]bool{}
	a.tileCols = map[string]int{}
	a.start()
}

// levelHeadline is the overview banner's headline and the reasons under
// it. When the health checks are the only cause, the headline is their
// own sentence (健康检查：2 项失败), the one the health page leads with,
// not the generic 出现故障 above a line that repeats it.
func levelHeadline(s status.Snapshot) (string, []status.Reason) {
	if len(s.Reasons) == 1 && s.Reasons[0].Health {
		return s.Reasons[0].Text, nil
	}
	return s.Level.Text(), s.Reasons
}
