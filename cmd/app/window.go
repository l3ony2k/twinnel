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
)

func (a *app) mainView(c *ui.Context) {
	a.applyTheme(c)
	t := c.Theme()
	if c.Shortcut(ui.Cmd, ui.KeyW) {
		a.win.Close()
		return
	}
	a.flushToast(c)
	s := a.mon.Snapshot()
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c, s)
		content := ui.Column(c).Grow(1).Background(t.Background)
		if c.Vibrancy() {
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
					ui.Column(c).Padding(barH+4, plat.pagePadX, 32).AlignItems(ui.Center).Children(func() {
						ui.Column(c).Gap(gapGroup).FillWidth().MaxWidth(plat.contentMax).Children(func() {
							a.pageBody(c, s)
						})
					})
				})
				scrolled = st.Y > 0.5
			}
			a.toolbar(c, barH, scrolled)
		})
	})
}

// The toolbar: the page's title and actions on the traffic lights' line,
// pinned while the page scrolls under it, as a unified toolbar on macOS
// (System Settings, Finder). At the top of the page it has no background;
// once content scrolls under it, the scroll edge of macOS 26 (content
// frosted under the window's background, a hairline below) sets it off.

type toolbarSpec struct {
	title    string
	trailing func()
}

// pageBar is what the current page puts in the toolbar (pageHeader);
// mainView builds the toolbar after the page. Main thread only.
var pageBar toolbarSpec

// toolbarHeight is the toolbar's height: the title bar's under a hidden
// title bar (the traffic lights centered in it), at least a unified
// toolbar's 52.
func toolbarHeight(c *ui.Context) float32 {
	return max(c.TitleBar().Height, 52)
}

func (a *app) toolbar(c *ui.Context, h float32, scrolled bool) {
	t := c.Theme()
	if scrolled { // fades in and out as macOS 26's scroll edge does
		ui.Box(c.Key("toolbar-edge")).Absolute().Top(0).Left(0).Right(0).Height(h).PassThrough().
			Material(glass.ScrollEdge{Hard: true, Background: t.Background}).Transition(fadeMotion)
	}
	spec := pageBar
	// The title and the actions line up with the page's column: over its
	// left and right edges at every width, as the column is centered and
	// no wider than contentMax. Logs fill the pane, and so does their bar.
	inner := float32(plat.contentMax)
	if a.page == pageLogs {
		inner = 0
	}
	ui.Column(c.Key("toolbar")).Absolute().Top(0).Left(0).Right(0).Height(h).PaddingX(plat.pagePadX).
		AlignItems(ui.Center).Justify(ui.Center).DragWindow().Role(ui.RoleToolbar).Label(spec.title).Children(func() {
		row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
		if inner > 0 {
			row.MaxWidth(inner)
		}
		row.Children(func() {
			ui.Text(c, spec.title).FontSize(plat.titleBarSize).FontWeight(plat.titleBarWeight).SingleLine().Shrink(1)
			ui.Spacer(c)
			if spec.trailing != nil {
				toolbarActions(c, spec.trailing)
			}
		})
	})
}

// inToolbar is true while the toolbar's actions are built: buttons there
// keep the toolbar's look, not a push button's (pushFace). Main thread
// only.
var inToolbar bool

// toolbarActions builds a page's toolbar actions. Under Liquid Glass
// (macOS 26+) they are faceless toolbar items on one glass capsule, as
// Finder's and System Settings'; elsewhere, the controls as built.
func toolbarActions(c *ui.Context, trailing func()) {
	if !liquidGlass {
		trailing()
		return
	}
	inToolbar = true
	defer func() { inToolbar = false }()
	g := glass.NewGroup(c)
	g.Radius = 16
	g.Shrink(0).Children(func() {
		g.Pane(ui.Toolbar(c, trailing).Height(32).Padding(0, 6).Gap(4).Shrink(0))
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
	}
}

// sidebar is the window's source list over the platform's sidebar
// material, with the duet pair's state at its foot.
func (a *app) sidebar(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	bar := c.TitleBar()
	side := ui.Column(c).Width(plat.sidebarWidth).Shrink(0).DragWindow()
	if !c.Vibrancy() { // an opaque stand-in for the material
		side.Background(ui.Hex("#ebebeb"))
		if t.Dark {
			side.Background(ui.Hex("#2a2a2c"))
		}
	}
	side.Padding(max(bar.Bottom+12, 16), 10, 12).Gap(4).Children(func() {
		old := a.page
		ts := 0
		if s.Tailscale.Reachable {
			ts = s.Tailscale.State.OnlinePeers()
		}
		w, f := s.Health.Counts()
		health := w + f
		healthTone := toneAttention
		if f > 0 {
			healthTone = toneAlert
		}
		sections := navSections(ts, health, healthTone)
		a.pageKeys(c, sections)
		active := c.WindowActive()
		if a.shotNav.active != nil {
			active = *a.shotNav.active
		}
		ui.Scroll(c).Grow(1).Children(func() {
			if navList(c, &a.page, sections, active, a.shotNav.hover) {
				a.pageChanged(old)
			}
		})
		a.duetPair(c, s, 6)
	})
}

// navSections are the sidebar's pages in order, with the counts their
// rows show (Tailscale devices online, health problems). The first nine
// pages get ⌘1…⌘9 (numberPages); 设置 has ⌘, as in every Mac app.
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
			{id: pageConfig, icon: iconConfig, label: T.PageConfig},
			{id: pageHistory, icon: iconHistory, label: T.PageHistory},
			{id: pageKernel, icon: iconKernel, label: T.PageKernel},
			{id: pageTakeover, icon: iconTakeover, label: T.PageTakeover},
		}},
		{title: T.SidebarApp, items: []navItem{
			{id: pageLogs, icon: iconLogs, label: T.PageLogs},
			{id: pageSettings, icon: iconSettings, label: T.PageSettings},
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

// --- overview ---

func (a *app) overviewPage(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	pageTitle(c, T.PageOverview)
	a.verdict(c, s)
	a.actionNotes(c, s)
	card(c, T.Traffic, func() { a.trafficChart(c, s) })
	ui.Row(c).Gap(gapGroup).AlignItems(ui.Stretch).Children(func() {
		cardWith(c, T.Mihomo, func() { a.mihomoMenu(c, s) }, func() { a.mihomoSummary(c, s) }).Basis(0).Grow(1)
		card(c, T.Tailscale, func() { a.tailscaleSummary(c, s) }).Basis(0).Grow(1)
	})
	card(c, T.Helper, func() { a.helperSummary(c, s) })
}

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
				ui.Text(c, s.Level.Text()).FontSize(15).FontWeight(600)
				for _, r := range s.Reasons {
					ui.Text(c, r.Text).TextColor(t.TextMuted).Wrap().Selectable()
				}
			})
		})
	})
}

// helperSummary is the overview's helper card.
func (a *app) helperSummary(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	h := s.Helper
	kv(c, T.State, h.State.Text())
	st := h.Status
	if !h.Connected() || st == nil {
		kv(c, T.HelperSocket, h.Socket)
		if h.State == status.HelperNotInstalled {
			// A push button in the value column, as other row actions;
			// not a bar across the card.
			kvWith(c, "", func() {
				if pushButton(c.Key("onboard"), T.OnboardOpen).Clicked() {
					old := a.page
					a.page = pageConfig
					a.pageChanged(old)
				}
			})
		}
		return
	}
	kv(c, T.Version, fmt.Sprintf("%s · pid %d", st.HelperVersion, st.PID))
	kv(c, T.Subscription, orDash(st.ActiveSubscription))
	kv(c, T.Layers, fmt.Sprint(st.Layers))
	if r := st.LastApply; r != nil {
		last := r.Time.Local().Format("01-02 15:04") + " · " + T.ApplyDone
		if r.Error != "" {
			last = r.Time.Local().Format("01-02 15:04") + " · " + T.ApplyFailed + "：" + r.Error
		}
		kv(c, T.LastApply, last)
	}
}

func (a *app) mihomoSummary(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	m := s.Mihomo
	if !m.Reachable {
		kv(c, T.State, T.Unreachable)
		unreachableLine(c, svcMihomo, m.Err)
		kv(c, T.Controller, a.mh.BaseURL())
		return
	}
	kv(c, T.State, T.Running)
	kv(c, T.Version, m.Version)
	kv(c, T.Mode, T.ModeName(string(m.Config.Mode)))
	tun := T.TunOff
	if m.Config.Tun.Enable {
		tun = m.Config.Tun.Device + sourceSuffix(m.Config.Tun.Stack) // the switch says it is on
	}
	kvWith(c, T.Tun, func() {
		a.tunSwitch(c, s)
		ui.Text(c, tun).TextColor(t.TextMuted).SingleLine().Shrink(1)
	})
	a.controlError(c, pendTun)
	if p := m.Primary; p != nil {
		// One row for where traffic leaves: the primary group, what it
		// selected down to the node, and the node's delay.
		// The node keeps its room; the groups before it shrink first.
		route := strings.Join(append([]string{p.Name}, p.Chain...), " → ")
		via := strings.Join(append([]string{p.Name}, p.Chain[:max(len(p.Chain)-1, 0)]...), " → ") + " →"
		kvWith(c, T.Exit, func() {
			ui.Row(c).Gap(4).Grow(1).Shrink(1).MinWidth(0).Tooltip(route).Children(func() {
				// The groups may shrink to nothing (the tooltip has the
				// route); the node keeps its whole name.
				ui.Text(c, via).TextColor(t.TextMuted).SingleLine().Shrink(100).MinWidth(0)
				ui.Text(c, p.Node).SingleLine().Shrink(0)
			})
			num(ui.Text(c, status.DelayText(p.Delay, p.Tested)).TextColor(delayColor(t, p.Delay, p.Tested)).Shrink(0))
		})
	}
	kv(c, T.Connections, fmt.Sprint(m.Connections))
}

func (a *app) tailscaleSummary(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	ts := s.Tailscale
	st := ts.State
	if !ts.Reachable {
		kv(c, T.State, T.Unreachable)
		unreachableLine(c, svcTailscale, ts.Err)
		kv(c, T.Install, T.InstallKind(string(ts.Install.Kind)))
		return
	}
	kv(c, T.State, T.TSState(st.BackendState))
	kv(c, T.SelfName, st.Self.Name)
	if ip := st.Self.IPv4(); ip.IsValid() {
		kv(c, T.SelfIP, ip.String())
	}
	kv(c, T.OnlinePeers, fmt.Sprintf("%d / %d", st.OnlinePeers(), len(st.Peers)))
	kvWith(c, T.ExitNode, func() { kvControl(a.exitNodePicker(c, s)) })
	a.controlError(c, pendExit)
	kv(c, T.MagicDNS, onOff(st.Prefs.MagicDNS))
	if st.KeyExpiry != nil {
		kv(c, T.KeyExpiry, st.KeyExpiry.Local().Format("2006-01-02")+" ("+status.Until(s.Time, *st.KeyExpiry)+")")
	}
	kv(c, T.Install, T.InstallKind(string(ts.Install.Kind))+sourceSuffix(ts.Install.Source))
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
		return fmt.Sprintf(i18n.T.TrafficLastSec, max(sec, 1))
	}
	return fmt.Sprintf(i18n.T.TrafficLastMin, sec/60)
}

// trafficChart draws the last two minutes of download and upload rates:
// the overview's one rich element. Download carries the accent; upload is
// a quiet second line.
func (a *app) trafficChart(c *ui.Context, s status.Snapshot) {
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
				num(ui.Text(c, status.Rate(rate)).FontSize(20).FontWeight(500))
			})
		}
		figure(T.Download, t.Accent, s.Mihomo.Traffic.Down)
		figure(T.Upload, up, s.Mihomo.Traffic.Up)
		ui.Spacer(c)
		if s.Mihomo.Reachable {
			ui.Column(c).Gap(2).AlignItems(ui.End).Children(func() {
				if len(hist) > 1 {
					num(small(c, trafficSpan(hist[len(hist)-1].Time.Sub(hist[0].Time))))
				}
				num(small(c, fmt.Sprintf("%s ↓ %s  ↑ %s", T.Total, status.Bytes(s.Mihomo.Traffic.DownTotal), status.Bytes(s.Mihomo.Traffic.UpTotal))))
			})
		}
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
	grid := pal(t).separator
	axes := chart.Axes{Right: ticks, Label: func(_ chart.Side, tk chart.Tick) {
		num(ui.Text(c, tk.Label).FontSize(plat.smallSize).TextColor(t.TextMuted).NoWrap())
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
			p.FillPath(chart.Area(r, xs, nil, down, chart.CurveMonotone), t.Accent.Alpha(0.14))
			p.StrokePath(chart.Line(r, xs, upv, chart.CurveMonotone), 1.5, up.Alpha(0.8))
			p.StrokePath(chart.Line(r, xs, down, chart.CurveMonotone), 2, t.Accent)
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
						num(ui.Text(c, status.Rate(rate)).FontSize(plat.bodySize - 1).NoWrap())
					})
				}
				readout(t.Accent, T.Download, h.Down)
				readout(up, T.Upload, h.Up)
				num(small(c, status.Ago(s.Time, h.Time)).NoWrap())
			})
		})
	}).Label(T.Traffic).Height(176).Gap(8)
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
		busy := false
		for _, g := range m.Groups {
			busy = busy || a.mon.Testing(g.Name)
		}
		// The button keeps its label and width; its icon turns into a
		// spinner while the groups are tested.
		b := pushButton(c.Key("test-all"), "").Label(T.TestAllDelay).Disabled(busy)
		spin := showBusy(c, b, busy)
		b.Children(func() {
			if spin {
				spinnerIcon(c)
			} else {
				ui.Icon(c, iconSpeed).FontSize(iconInline)
			}
			ui.Text(c, T.TestAllDelay).SingleLine()
		})
		if b.Clicked() {
			names, mon := make([]string, 0, len(m.Groups)), a.mon
			for _, g := range m.Groups {
				names = append(names, g.Name)
			}
			go func() { // one group at a time: the controller tests every node of each
				for _, name := range names {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					_ = mon.TestGroup(ctx, name)
					cancel()
				}
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
			a.groupRow(c.Key("group:"+g.Name), g, s.WritesAllowed)
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
	ui.Column(c).Gap(8).PaddingY(8).Clip().Transition(expandFor(open)).Children(func() {
		// The head is a disclosure the keyboard reaches: Tab stops on it,
		// Return or Space opens and closes the group.
		head := ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(28).Cursor(ui.CursorPointer).
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
			ui.Text(c, g.Name).FontWeight(600).SingleLine().Shrink(0)
			meta(c, g.Type)
			ui.Text(c, chain).TextColor(t.TextMuted).SingleLine().Shrink(1).Tooltip(chain)
			ui.Spacer(c)
			num(ui.Text(c, status.DelayText(g.Delay, g.Tested)).TextColor(delayColor(t, g.Delay, g.Tested)).Shrink(0))
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
	delay := status.DelayText(mem.Delay, mem.Tested)
	desc := mem.Name + " · " + typ + " · " + delay
	canSelect := writes && g.Selectable() && !selected
	// Every tile takes the focus, so the keyboard and VoiceOver can read
	// the whole group; one that cannot be picked (read-only mode, or a
	// group mihomo picks for itself) says so as unavailable.
	chip := ui.Column(c).MinWidth(0).Padding(8, 10).Gap(2).Radius(plat.innerRadius).
		Background(p.code).Border(1, p.groupLine).Transition(colorMotion).
		Role(ui.RoleRadio).Label(desc).AccessSelected(selected).Tooltip(desc).
		Focusable().AccessDisabled(!canSelect && !selected)
	if selected {
		chip.Background(p.accentSoft).Border(1, t.Accent.Alpha(0.35))
	}
	if canSelect {
		chip.Cursor(ui.CursorPointer)
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
		if m.Item(T.TestNodeDelay).Chosen() {
			a.testNode(mem.Name)
		}
		if m.Item(T.CopyNodeName).Chosen() {
			copyText(c, mem.Name)
		}
	})
	chip.Children(func() {
		ui.Text(c, mem.Name).SingleLine()
		ui.Row(c).Gap(6).Children(func() {
			meta(c, typ).Grow(1).Shrink(1)
			num(ui.Text(c, delay).FontSize(plat.smallSize).TextColor(delayColor(t, mem.Delay, mem.Tested)))
		})
	})
	if chip.Clicked() && canSelect {
		a.selectNode(g.Name, mem.Name)
	}
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

// testGroup delay-tests every node of a group (a read: the controller's
// GET /group/<name>/delay) and opens the group to show the results.
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
				kv(c, T.Version, st.Version)
				kv(c, T.Install, T.InstallKind(string(ts.Install.Kind))+sourceSuffix(ts.Install.Source))
			})
			ui.Column(c).Gap(gapRow).Basis(0).Grow(1).Children(func() {
				kv(c, T.MagicDNS, onOff(st.Prefs.MagicDNS))
				kv(c, T.ShieldsUp, onOff(st.Prefs.ShieldsUp))
				kvWith(c, T.ExitNode, func() { kvControl(a.exitNodePicker(c, s)) })
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
	card(c, title, func() {
		if len(peers) == 0 {
			// In a card under the account's: the card size of an empty
			// state (one row, the title alone), not a page's.
			cardEmpty(c, iconTailscale, T.NoPeers).Tooltip(T.NoPeersHint)
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
		// narrow window (900 px) and the last column is never cut.
		cols := []ui.TableColumn{
			{Title: T.PeerName, MinWidth: 100},
			{Title: T.PeerIP, Width: 140},
			{Title: T.PeerOS, Width: 72},
			{Title: T.PeerPath, Width: 108},
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
				num(ui.Text(c, ip).SingleLine().Selectable())
			case 2:
				ui.Text(c, p.OS).TextColor(t.TextMuted).SingleLine()
			case 3:
				icon, col := (*ui.SVG)(nil), t.TextMuted
				switch p.Path() {
				case "direct":
					icon, col = iconPathDirect, t.Text
				case "derp", "peer-relay":
					icon, col = iconPathRelay, pal(t).warnText
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if icon != nil {
						ui.Icon(c, icon).FontSize(iconInline).TextColor(col)
					}
					ui.Text(c, T.Path(p.Path())).TextColor(col).SingleLine()
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
	mihomoBinary, mihomoConfigDir        string
	msg                                  string
	failed                               bool
}

func (f *settingsForm) load(c status.Config) {
	*f = settingsForm{
		controller: c.Controller, primary: c.PrimaryGroup, testURL: c.TestURL,
		mihomoBinary: c.MihomoBinary, mihomoConfigDir: c.MihomoConfigDir,
	}
	if !c.SecretFromEnv {
		f.secret = c.Secret
	}
}

func (a *app) settingsPage(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	f := &a.settings
	pageTitle(c, T.PageSettings)
	field := func(key, label, hint string, v *string, password bool) {
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Text(c, label).TextColor(t.TextMuted).Width(labelWidth + 24).Shrink(0).MarginY(5)
			ui.Column(c).Gap(4).Grow(1).Children(func() {
				in := ui.TextInput(c.Key(key), v).Label(label)
				if password {
					in.Password()
				}
				if hint != "" {
					small(c, hint).Wrap()
				}
			})
		})
	}
	card(c, T.Mihomo, func() {
		if a.creds.cur != nil {
			small(c, T.ControllerFromHelper).Wrap()
		}
		field("controller", T.Controller, "", &f.controller, false)
		secretHint := T.SecretHint
		if a.cfg.SecretFromEnv {
			secretHint = T.SecretFromEnv
		}
		field("secret", T.Secret, secretHint, &f.secret, true)
		field("primary", T.PrimaryGroup, "", &f.primary, false)
		field("binary", T.MihomoBinary, "", &f.mihomoBinary, false)
		field("confdir", T.MihomoConfigDir, "", &f.mihomoConfigDir, false)
		ui.Row(c).Gap(12).AlignItems(ui.Center).PaddingStart(labelWidth + 36).Children(func() {
			if ui.PrimaryButton(c.Key("save"), T.Save).Clicked() {
				a.saveSettings()
			}
			small(c, T.SettingsRestartHint).Grow(1).Wrap()
		})
		if f.msg != "" && f.failed {
			errorNote(c, f.msg)
		}
	})
	card(c, T.Notifications, func() {
		on := !a.cfg.NotificationsOff
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, T.NotificationsSwitch)
				small(c, T.NotificationsHint).Wrap()
			})
			if ui.Switch(c.Key("notify"), &on).Label(T.Notifications).Disabled(!notificationsSupported()).Changed() {
				a.setNotifications(on)
			}
		})
		if !notificationsSupported() {
			small(c, T.NotificationsUnsupported).Wrap()
		}
	})
	card(c, T.Tailscale, func() {
		in := s.Tailscale.Install
		kv(c, T.Install, T.InstallKind(string(in.Kind))+sourceSuffix(in.Source))
		if in.Daemon != "" {
			kv(c, "tailscaled", in.Daemon)
		}
	})
	card(c, T.AboutApp, func() {
		kv(c, T.ConfigFile, a.cfgPath)
		kv(c, T.AllowWrites, onOff(a.cfg.AllowWrites))
	})
}

func (a *app) setNotifications(on bool) {
	cfg := a.cfg
	cfg.NotificationsOff = !on
	if err := status.SaveConfig(a.cfgPath, cfg); err != nil {
		a.settings.msg, a.settings.failed = i18n.T.SaveFailed+err.Error(), true
		return
	}
	a.cfg = cfg
}

// actionNotes shows the read-only notice and the last action's error.
func (a *app) actionNotes(c *ui.Context, s status.Snapshot) {
	if !s.WritesAllowed {
		notice(c, iconSecure, i18n.T.WritesDisabledHint)
	}
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
	cfg.MihomoBinary = strings.TrimSpace(f.mihomoBinary)
	cfg.MihomoConfigDir = strings.TrimSpace(f.mihomoConfigDir)
	if !cfg.SecretFromEnv {
		cfg.Secret = f.secret
	}
	if cfg.PrimaryGroup == "" {
		cfg.PrimaryGroup = status.DefaultPrimaryGroup
	}
	if _, err := mihomo.New(mihomo.Options{Address: cfg.Controller}); err != nil {
		f.msg, f.failed = i18n.T.SaveFailed+err.Error(), true
		return
	}
	if err := status.SaveConfig(a.cfgPath, cfg); err != nil {
		f.msg, f.failed = i18n.T.SaveFailed+err.Error(), true
		return
	}
	a.cfg = cfg
	f.msg, f.failed = "", false
	a.toast = i18n.T.Saved
	a.groupsOpen = map[string]bool{}
	a.tileCols = map[string]int{}
	a.start()
}
