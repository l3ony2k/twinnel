package main

import (
	"context"
	"fmt"
	"strings"
	"time"

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
		content.Children(func() {
			switch a.page {
			case pageLogs:
				a.logsPage(c) // has its own scrolling list
			default:
				ui.Scroll(c).Grow(1).Children(func() {
					ui.Column(c).Padding(pagePadTop(c), plat.pagePadX, 32).AlignItems(ui.Center).Children(func() {
						ui.Column(c).Gap(gapGroup).FillWidth().MaxWidth(plat.contentMax).Children(func() {
							a.pageBody(c, s)
						})
					})
				})
			}
		})
	})
}

// pagePadTop is the room above a page's title. Under a hidden title bar
// the title row (32 high) sits on the traffic lights' line, as the title of
// a unified toolbar does, and drags the window (pageHeader).
func pagePadTop(c *ui.Context) float32 {
	if bar := c.TitleBar(); bar.Height > 0 {
		return max((bar.Height-32)/2, 4)
	}
	return plat.pagePadTop
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
	T := i18n.T
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
		ts, health := 0, 0
		if s.Tailscale.Reachable {
			ts = s.Tailscale.State.OnlinePeers()
		}
		w, f := s.Health.Counts()
		health = w + f
		healthTone := toneAttention
		if f > 0 {
			healthTone = toneAlert
		}
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

// duetPair is the signature: the two networks side by side, each with its
// state dot, and the helper under them.
func (a *app) duetPair(c *ui.Context, s status.Snapshot, padX float32) {
	t := c.Theme()
	T := i18n.T
	mcol := t.Danger
	if s.Mihomo.Reachable {
		mcol = t.Success
	}
	ui.Column(c).Gap(6).Padding(8, padX).Children(func() {
		pair := func(name string, col ui.Color) {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				dot(c, col, 7)
				ui.Text(c, name).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine()
			})
		}
		ui.Row(c).Gap(14).Children(func() {
			pair(T.Mihomo, mcol)
			pair(T.Tailscale, tsColor(t, s))
		})
		hcol := t.TextMuted
		switch s.Helper.State {
		case status.HelperConnected:
			hcol = t.Success
		case status.HelperNotRunning, status.HelperDenied, status.HelperNotInstalled:
			hcol = t.Warning
		}
		pair(T.Helper+" · "+s.Helper.State.Text(), hcol)
	})
}

// tsColor is the dot color of Tailscale's state.
func tsColor(t *ui.Theme, s status.Snapshot) ui.Color {
	ts := s.Tailscale
	switch {
	case !ts.Reachable:
		return t.Danger
	case ts.State.Running():
		return t.Success
	case ts.State.BackendState == "Stopped" || ts.State.BackendState == "Starting":
		return t.Warning
	}
	return t.Danger
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
			ui.Icon(c, icon).FontSize(22).TextColor(levelColor(t, s.Level)).Shrink(0)
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
		if h.State == status.HelperNotInstalled && ui.Button(c.Key("onboard"), T.OnboardOpen).Clicked() {
			old := a.page
			a.page = pageConfig
			a.pageChanged(old)
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
		if m.Err != "" {
			errorNote(c, m.Err)
		}
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
	if p := m.Primary; p != nil {
		kvWith(c, p.Name, func() {
			ui.Text(c, p.Node).SingleLine().Shrink(1)
			num(ui.Text(c, status.DelayText(p.Delay, p.Tested)).TextColor(delayColor(t, p.Delay, p.Tested)).Shrink(0))
		})
		if len(p.Chain) > 1 {
			kv(c, T.Exit, strings.Join(p.Chain, " → "))
		}
	}
	kv(c, T.Connections, fmt.Sprint(m.Connections))
}

func (a *app) tailscaleSummary(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	ts := s.Tailscale
	st := ts.State
	if !ts.Reachable {
		kv(c, T.State, T.Unreachable)
		if ts.Err != "" {
			errorNote(c, ts.Err)
		}
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
	ui.Row(c).Gap(28).AlignItems(ui.End).Children(func() {
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
	y := chart.NewLinear(0, hi).Nice(3)
	ticks := y.Ticks(3)
	for i := range ticks {
		ticks[i].Label = status.Rate(int64(ticks[i].Value))
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
		down[i] = y.At(float64(h.Down))
		upv[i] = y.At(float64(h.Up))
	}
	grid := pal(t).separator
	axes := chart.Axes{Right: ticks, Label: func(_ chart.Side, tk chart.Tick) {
		num(ui.Text(c, tk.Label).FontSize(plat.smallSize).TextColor(t.TextMuted).NoWrap())
	}}
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
	}).Label(T.Traffic).Height(176).Gap(8)
}

// --- proxies ---

func (a *app) proxiesPage(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	pageTitle(c, T.PageProxies)
	m := s.Mihomo
	if !m.Reachable {
		groupBox(c, t).Children(func() {
			emptyState(c, iconError, T.Unreachable, m.Err)
		})
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

// Node tiles: columns of equal width that fill the row, each tile at
// least tileMin wide (so at most about twice that before another column
// fits).
const (
	tileMin = 168
	tileGap = 8
)

// tileColumns is how many tile columns fit in w; 4 before the first
// layout.
func tileColumns(w float32) int {
	if w <= 0 {
		return 4
	}
	return max(1, int((w+tileGap)/(tileMin+tileGap)))
}

func (a *app) groupRow(c *ui.Context, g status.Group, writes bool) {
	t := c.Theme()
	T := i18n.T
	p := pal(t)
	open := a.groupsOpen[g.Name]
	ui.Column(c).Gap(10).PaddingY(8).Clip().Transition(expandMotion).Children(func() {
		head := ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(28).Cursor(ui.CursorPointer)
		head.Children(func() {
			chev := iconCollapsed
			if open {
				chev = iconExpanded
			}
			ui.Icon(c, chev).FontSize(12).TextColor(t.TextMuted).Width(14)
			ui.Text(c, g.Name).FontWeight(600).SingleLine().Shrink(0)
			meta(c, g.Type)
			ui.Text(c, strings.Join(g.Chain, " → ")).TextColor(t.TextMuted).SingleLine().Shrink(1)
			ui.Spacer(c)
			num(ui.Text(c, status.DelayText(g.Delay, g.Tested)).TextColor(delayColor(t, g.Delay, g.Tested)).Shrink(0))
			testing := a.mon.Testing(g.Name)
			label := T.TestGroupDelay
			if testing {
				label = T.TestingDelay
			}
			if iconButton(c.Key("test"), iconSpeed, label, false).Disabled(testing).Clicked() {
				name, mon := g.Name, a.mon
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					_ = mon.TestGroup(ctx, name)
				}()
				a.groupsOpen[g.Name] = true
			}
		})
		if head.Clicked() {
			a.groupsOpen[g.Name] = !open
		}
		if !open {
			return
		}
		grid := ui.Grid(c.Key("tiles")).Gap(tileGap).MarginStart(24).MarginY(2)
		cols := tileColumns(grid.Bounds().W)
		if cols != a.tileCols[g.Name] { // the width changed: lay out again with the new count
			a.tileCols[g.Name] = cols
			c.Invalidate()
		}
		grid.Columns(cols).Children(func() {
			for _, mem := range g.Members {
				selected := mem.Name == g.Now
				chip := ui.Column(c.Key(mem.Name)).MinWidth(0).Padding(8, 10).Gap(2).Radius(8).
					Background(p.code).Border(1, p.groupLine).Transition(ui.ElementTransition{})
				if selected {
					chip.Background(p.accentSoft).Border(1, t.Accent.Alpha(0.6))
				}
				canSelect := writes && g.Selectable() && !selected
				if canSelect {
					chip.Cursor(ui.CursorPointer)
					if chip.Hovered() {
						chip.Background(t.SurfaceHover)
					}
				}
				chip.Children(func() {
					ui.Text(c, mem.Name).SingleLine().FontWeight(map[bool]int{true: 600, false: 400}[selected])
					ui.Row(c).Gap(6).Children(func() {
						typ := mem.Type
						if typ == "" {
							typ = "?"
						}
						meta(c, typ).Grow(1).Shrink(1)
						num(ui.Text(c, status.DelayText(mem.Delay, mem.Tested)).FontSize(plat.smallSize).TextColor(delayColor(t, mem.Delay, mem.Tested)))
					})
				})
				if chip.Clicked() && canSelect {
					group, name := g.Name, mem.Name
					a.do(func(ctx context.Context) error { return a.mh.SelectProxy(ctx, group, name) })
				}
			}
		})
	})
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
		groupBox(c, t).Children(func() { emptyState(c, iconError, T.Unreachable, ts.Err) })
		return
	}
	a.actionNotes(c, s)
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
	})
	peers := st.Peers
	title := fmt.Sprintf("%s · %d / %d", T.Devices, st.OnlinePeers(), len(peers))
	card(c, title, func() {
		if len(peers) == 0 {
			emptyState(c, iconTailscale, T.NoPeers, T.NoPeersHint)
			return
		}
		a.peers.RowHeight = 32
		cols := []ui.TableColumn{
			{Title: T.PeerName, Width: 170},
			{Title: T.PeerIP, Width: 140},
			{Title: T.PeerOS, Width: 80},
			{Title: T.PeerPath, Width: 120},
			{Title: T.PeerRelay, Width: 70},
			{Title: T.PeerLastSeen},
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
					ui.Text(c, p.Name).SingleLine()
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
						ui.Icon(c, icon).FontSize(14).TextColor(col)
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

// --- logs ---

var logLevels = []string{"debug", "info", "warning", "error"}

type logsView struct {
	level  int // index into logLevels; zero value means "info" via init
	inited bool
	paused bool
	frozen []mihomo.LogEntry
	list   ui.ListState
}

func (l *logsView) levelName() string {
	if !l.inited {
		l.level, l.inited = 1, true
	}
	return logLevels[l.level]
}

func (a *app) logsPage(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	p := pal(t)
	a.logs.levelName()
	entries := a.mon.Logs()
	if a.logs.paused {
		entries = a.logs.frozen
	}
	ui.Column(c).Grow(1).Padding(pagePadTop(c), plat.pagePadX, 20).Gap(16).Children(func() {
		pageHeader(c, T.PageLogs, func() {
			small(c, T.LogLevel)
			if ui.Segmented(c.Key("level"), &a.logs.level, logLevels...).Changed() {
				a.mon.ClearLogs()
				a.mon.SetLogLevel(a.logs.levelName())
			}
			pause := T.LogPause
			if a.logs.paused {
				pause = T.LogResume
			}
			if ui.Button(c.Key("pause"), pause).Clicked() {
				a.logs.paused = !a.logs.paused
				a.logs.frozen = a.mon.Logs()
			}
			if ui.Button(c.Key("clear"), T.LogClear).Clicked() {
				a.mon.ClearLogs()
				a.logs.frozen = nil
			}
		})
		box := ui.Column(c).Grow(1).Radius(plat.groupRadius).Background(p.group).Border(1, p.groupLine).Clip()
		box.Children(func() {
			if len(entries) == 0 {
				ui.Column(c).Grow(1).Justify(ui.Center).Children(func() {
					emptyState(c, iconLogs, T.LogEmpty, T.LogEmptyHint)
				})
				return
			}
			ui.List(c, &a.logs.list, len(entries), func(i int) {
				e := entries[i]
				col := t.Text
				switch e.Type {
				case "warning":
					col = p.warnText
				case "error":
					col = p.badText
				case "debug":
					col = t.TextMuted
				}
				ui.Row(c).Gap(12).Padding(3, 12).AlignItems(ui.Start).Children(func() {
					ui.Text(c, e.Time.Format("15:04:05")).Font("monospace").FontSize(11).TextColor(t.TextMuted).Shrink(0)
					ui.Text(c, e.Type).Font("monospace").FontSize(11).TextColor(col).Width(56).Shrink(0)
					ui.Text(c, e.Payload).Font("monospace").FontSize(11).TextColor(col).Grow(1).Wrap().Selectable()
				})
			}).Grow(1).Background(ui.Transparent).PaddingY(6)
		})
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
