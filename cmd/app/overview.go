package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// 总览: what a mihomo + Tailscale user glances at, state first, then the
// few things they act on.
//
//   - the banner (one health number, at most two actions);
//   - a strip of quick actions;
//   - four glance tiles: the exit's delay, connections, mihomo's memory,
//     online devices (each opens its page);
//   - the traffic chart, smaller than before: the page's one rich element;
//   - mihomo | Tailscale, then duet | 最近活动, side by side when there is
//     room. Every card has a title and the same slot for a trailing action.
//
// Figures, addresses, ports and versions are monospaced (ovMono).

// ovMaxWidth is the widest the overview grows: a dashboard uses the
// window, where the other pages keep plat.contentMax for reading.
const ovMaxWidth = 1280

// Breakpoints of the overview's grids, in the page column's width.
const (
	ovTilesWide = 720 // four tiles in a row from here, else two
	ovTileGap   = 12
)

// pageMaxWidth is how wide a page's content may grow.
func pageMaxWidth(page string) float32 {
	if page == pageOverview {
		return max(plat.contentMax, ovMaxWidth)
	}
	return plat.contentMax
}

// ovMono is monospaced text with digits of one width: figures, addresses,
// ports, versions.
func ovMono(c *ui.Context, s string) ui.Element {
	return num(ui.Text(c, s).Font("monospace"))
}

func (a *app) overviewPage(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	pageTitle(c, T.PageOverview)
	a.backfillEvents() // 最近活动 reads the helper's recent events
	w := a.ovWidth
	tiles := 2
	if w == 0 || w >= ovTilesWide {
		tiles = 4
	}
	pair := w == 0 || w >= 2*max(halfCardMin, 300)+gapGroup
	col := ui.Column(c.Key("overview")).Gap(gapGroup).FillWidth()
	col.Children(func() {
		a.verdict(c, s)
		a.exitNodeAcceptedRow(c, s)
		a.gettingStarted(c, s)
		a.actionNotes(c, s)
		a.quickActions(c, s)
		a.glanceTiles(c, s, tiles)
		ovCard(c, T.Traffic, "", func() { a.trafficCaption(c, s) }, func() { a.trafficChart(c, s, 120) })
		ovPair(c, pair,
			func() ui.Element { return a.mihomoCard(c, s) },
			func() ui.Element { return a.tailscaleCard(c, s) })
		ovPair(c, pair,
			func() ui.Element { return a.duetCard(c, s) },
			func() ui.Element { return a.recentCard(c, s) })
	})
	if bw := col.Bounds().W; bw > 0 && bw != a.ovWidth { // the width changed: lay out again
		a.ovWidth = bw
		c.Invalidate()
	}
}

// ovCard is a card of the overview: the title (and ⓘ) with an optional
// trailing action on one 28-high row, then the box, which grows to its
// neighbour's height in a pair.
func ovCard(c *ui.Context, title, tip string, trailing func(), body func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(groupPadX).Height(28).Children(func() {
			ui.Text(c, title).FontSize(plat.headingSize).FontWeight(plat.headingWeight).SingleLine()
			if tip != "" {
				infoTipAbout(c, title, tip, nil)
			}
			ui.Spacer(c)
			if trailing != nil {
				trailing()
			}
		})
		groupBox(c, t).Grow(1).Children(body)
	})
}

// ovPair puts two cards side by side, equal widths and heights, or one
// above the other when the page is too narrow for both.
func ovPair(c *ui.Context, side bool, left, right func() ui.Element) {
	if !side {
		left()
		right()
		return
	}
	ui.Row(c).Gap(gapGroup).AlignItems(ui.Stretch).Children(func() {
		left().Basis(0).Grow(1).MinWidth(0)
		right().Basis(0).Grow(1).MinWidth(0)
	})
}

// --- quick actions ---

// quickActions is one row of compact buttons for what a user does most
// from here. A button whose target can't act is disabled; one that needs
// the helper while it is not connected shows the lock after it.
func (a *app) quickActions(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	m := s.Mihomo
	can := s.Can()
	ui.Row(c.Key("quick")).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		// 全部测速: one batch (status.Monitor.TestAll), the icon a spinner
		// while it runs, as on 策略组.
		_, _, busy := a.mon.DelayProgress()
		b := pushButton(c.Key("test-all"), "").Label(T.TestAllDelay).Disabled(busy || !m.Reachable || len(m.Groups) == 0)
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
			a.testAll()
		}
		env := proxyEnv(m.Config)
		cp := iconButton(c.Key("copy-env"), iconCopy, T.OvCopyProxyEnv, false).Disabled(!m.Reachable || env == "")
		if env != "" {
			cp.Tooltip(env)
		}
		if cp.Clicked() {
			copyText(c, env)
		}
		if iconButton(c.Key("dashboard"), iconDashboard, T.OvDashboard, false).Disabled(!m.Reachable).Clicked() {
			_ = mygo.Shell.OpenExternal(dashboardURL(a.mh.BaseURL()))
		}
		if iconButton(c.Key("reload"), iconRefresh, T.ReloadConfig, false).Disabled(!can.Mihomo).Clicked() {
			a.reloadMihomo(s)
		}
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			if iconButton(c.Key("restart"), iconPower, T.RestartMihomo, false).Disabled(!can.Helper || a.hd.busy != "").Clicked() {
				a.confirmRestartMihomo()
			}
			a.helperLock(c, s)
		})
		pushFace(c, a.recoveryMenu(c, s))
	})
}

// testAll tests every node once in the background.
func (a *app) testAll() {
	mon := a.mon
	go func() {
		// The tests are bound to the monitor: they stop when the app
		// quits; this only bounds the wait.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_ = mon.TestAll(ctx)
	}()
}

// proxyEnv is the shell line that sends a terminal's traffic through
// mihomo: its mixed port (else the HTTP port, and the SOCKS port for
// all_proxy). "" when mihomo listens on none.
func proxyEnv(cfg mihomo.Config) string {
	const host = "127.0.0.1"
	httpPort, socksPort := cfg.MixedPort, cfg.MixedPort
	if httpPort == 0 {
		httpPort, socksPort = cfg.Port, cfg.SocksPort
	}
	var parts []string
	if httpPort > 0 {
		u := "http://" + host + ":" + strconv.Itoa(httpPort)
		parts = append(parts, "https_proxy="+u, "http_proxy="+u)
	}
	if socksPort > 0 {
		parts = append(parts, "all_proxy=socks5://"+host+":"+strconv.Itoa(socksPort))
	}
	if len(parts) == 0 {
		return ""
	}
	return "export " + strings.Join(parts, " ")
}

// dashboardURL is the external UI mihomo serves under its controller.
func dashboardURL(base string) string {
	return strings.TrimSuffix(base, "/") + "/ui/"
}

// confirmRestartMihomo asks before restarting mihomo: connections drop.
func (a *app) confirmRestartMihomo() {
	T := i18n.T
	parent := a.win
	go func() { // Dialog.Message blocks until answered
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:             parent,
			Type:               mygo.MessageWarning,
			Message:            T.OvRestartTitle,
			Detail:             T.OvRestartBody,
			Buttons:            []string{T.Cancel, T.RestartMihomo},
			DestructiveButtons: []int{1},
			CancelButton:       0,
			DefaultButton:      0,
		})
		if err == nil && res.Button == 1 {
			mygo.RunOnMain(func() { a.mihomoService(rpc.MethodServiceRestart, T.RestartMihomo) })
		}
	}()
}

// --- glance tiles ---

type ovTile struct {
	key, label string
	figure     string
	figColor   ui.Color // zero: the text color
	sub, tip   string
	page       string // where a click goes; "" for none
}

// glanceTiles are the numbers a user checks first, cols to a row.
func (a *app) glanceTiles(c *ui.Context, s status.Snapshot, cols int) {
	t := c.Theme()
	T := i18n.T
	m, ts := s.Mihomo, s.Tailscale
	exit := ovTile{key: "tile-exit", label: T.OvTileExit, figure: status.NoDelay, page: pageProxies}
	conns := ovTile{key: "tile-conns", label: T.Connections, figure: status.NoDelay}
	mem := ovTile{key: "tile-mem", label: T.OvTileMemory, figure: status.NoDelay, page: pageKernel}
	peers := ovTile{key: "tile-peers", label: T.OnlinePeers, figure: status.NoDelay, page: pageTailscale}
	if m.Reachable {
		if p := m.Primary; p != nil {
			exit.figure, exit.sub = p.DelayText(), p.Node
			exit.figColor = delayColor(t, p.Delay, p.Tested)
			exit.tip = strings.Join(append([]string{p.Name}, p.Chain...), " → ")
		}
		conns.figure = strconv.Itoa(m.Connections)
		conns.sub = fmt.Sprintf(T.OvTileTotal, status.Bytes(m.Traffic.DownTotal), status.Bytes(m.Traffic.UpTotal))
		conns.tip = T.Total + " " + conns.sub
		if b := mihomoMemory(m); b > 0 {
			mem.figure = status.Bytes(b)
		}
		if p := m.Process; p != nil {
			mem.sub = cpuText(p)
		}
		if m.Process != nil && m.Memory > 0 {
			mem.tip = fmt.Sprintf(T.OvMemoryTip, status.Bytes(m.Memory), status.Bytes(m.Process.RSS))
		}
	} else {
		exit.sub, conns.sub, mem.sub = T.Unreachable, T.Unreachable, T.Unreachable
	}
	if ts.Reachable && ts.State.Running() {
		peers.figure = fmt.Sprintf("%d / %d", ts.State.OnlinePeers(), len(ts.State.Peers))
		if p := ts.Process; p != nil {
			peers.sub = "tailscaled " + status.Bytes(p.RSS)
		}
	} else {
		peers.sub = T.TSState(ts.State.BackendState)
		if !ts.Reachable {
			peers.sub = T.Unreachable
		}
	}
	ui.Grid(c.Key("tiles")).Columns(cols).Gap(ovTileGap).Children(func() {
		for _, tl := range []ovTile{exit, conns, mem, peers} {
			a.tile(c, tl)
		}
	})
}

// mihomoMemory is mihomo's memory as the system counts it (resident, what
// Activity Monitor shows), else as mihomo reports it.
func mihomoMemory(m status.Mihomo) int64 {
	if m.Process != nil && m.Process.RSS > 0 {
		return m.Process.RSS
	}
	return m.Memory
}

// cpuText is a daemon's CPU use: "CPU 1.8%".
func cpuText(p *status.Process) string {
	return fmt.Sprintf(i18n.T.OvCPU, strconv.FormatFloat(p.CPU, 'f', 1, 64)+"%")
}

// processRows are a daemon's rows in its card: memory and CPU, then how
// long it has run.
func processRows(c *ui.Context, p *status.Process, now time.Time) {
	T := i18n.T
	kvWith(c, T.OvProcess, func() {
		num(ui.Text(c, status.Bytes(p.RSS)+" · "+cpuText(p))).SingleLine().Shrink(1)
	})
	kvWith(c, T.OvUptime, func() { num(ui.Text(c, status.Duration(p.Uptime(now)))).SingleLine().Shrink(1) })
}

// tile is one glance tile: a label, a figure, a quiet line under it. A
// tile with a page opens it (the arrow cursor, as every row).
func (a *app) tile(c *ui.Context, tl ovTile) {
	t := c.Theme()
	p := pal(t)
	b := ui.Column(c.Key(tl.key)).Gap(4).Padding(12, groupPadX).Radius(plat.groupRadius).
		Background(p.group).Border(1, p.groupLine).MinWidth(0)
	if tl.page != "" {
		b.Role(ui.RoleButton).Focusable().Label(tl.label + " " + tl.figure)
		if b.Hovered() {
			b.Border(1, t.Text.Alpha(0.18))
		}
		if b.Clicked() {
			a.openPage(tl.page)
		}
	}
	if tl.tip != "" {
		b.Tooltip(tl.tip)
	}
	b.Children(func() {
		small(c, tl.label).SingleLine()
		fig := num(ui.Text(c, tl.figure)).FontSize(plat.figureSize).FontWeight(500).SingleLine()
		if tl.figColor != (ui.Color{}) {
			fig.TextColor(tl.figColor)
		}
		sub := tl.sub
		if sub == "" {
			sub = " " // keep the tile's height
		}
		small(c, sub).SingleLine()
	})
}

// --- traffic ---

// trafficCaption is the traffic card's trailing text: how much time the
// chart shows.
func (a *app) trafficCaption(c *ui.Context, s status.Snapshot) {
	if !s.Mihomo.Reachable {
		return
	}
	if hist := a.mon.TrafficHistory(); len(hist) > 1 {
		num(small(c, trafficSpan(hist[len(hist)-1].Time.Sub(hist[0].Time)))).SingleLine()
	}
}

// --- mihomo ---

func (a *app) mihomoCard(c *ui.Context, s status.Snapshot) ui.Element {
	return ovCard(c, i18n.T.Mihomo, "", func() { a.mihomoMenu(c, s) }, func() { a.mihomoSummary(c, s) })
}

func (a *app) mihomoSummary(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	m := s.Mihomo
	if !m.Reachable {
		kv(c, T.State, T.Unreachable)
		unreachableLine(c, svcMihomo, m.Err)
		kvWith(c, T.Controller, func() { ovMono(c, a.mh.BaseURL()).SingleLine().Shrink(1).Selectable() })
		return
	}
	kvWith(c, T.State, func() {
		ui.Text(c, T.Running).SingleLine().Shrink(0)
		if m.Version != "" {
			ovMono(c, m.Version).TextColor(t.TextMuted).SingleLine().Shrink(1).Selectable()
		}
	})
	if p := m.Process; p != nil {
		processRows(c, p, s.Time)
	}
	// The mode at its own width: a segmented control never stretches.
	kvWith(c, T.Mode, func() { a.modeControl(c, s).Shrink(0) })
	a.controlError(c, pendMode)
	tun := T.TunOff
	if m.Config.Tun.Enable {
		tun = m.Config.Tun.Device + sourceSuffix(m.Config.Tun.Stack) // the switch says it is on
	}
	kvTipWith(c, T.Tun, T.InfoTun, nil, func() {
		a.tunSwitch(c, s)
		ovMono(c, tun).TextColor(t.TextMuted).SingleLine().Shrink(1)
	})
	a.controlError(c, pendTun)
	if d := dnsModeText(m.DNSMode); d != "" {
		kvWith(c, T.OvDNS, func() { ovMono(c, d).SingleLine() })
	}
	if ports := portsText(m.Config); ports != "" {
		kvWith(c, T.OvProxyPort, func() { ovMono(c, ports).SingleLine().Shrink(1).Selectable() })
	}
	if p := m.Primary; p != nil {
		// Where traffic leaves: the node and its delay; the route to it
		// on hover.
		route := strings.Join(append([]string{p.Name}, p.Chain...), " → ")
		kvWith(c, T.Exit, func() {
			ui.Text(c, p.Node).SingleLine().Shrink(1).MinWidth(0).Tooltip(route)
			ui.Spacer(c)
			ovMono(c, p.DelayText()).TextColor(delayColor(t, p.Delay, p.Tested)).Shrink(0)
		})
	}
}

// dnsModeText is the DNS mode as shown: mihomo's own words for its
// enhanced modes, words for the others; "" when unknown.
func dnsModeText(mode string) string {
	switch mode {
	case "":
		return ""
	case status.DNSModeOff:
		return i18n.T.OvDNSOff
	case status.DNSModeNormal:
		return i18n.T.OvDNSNormal
	}
	return mode
}

// portsText is where mihomo listens for proxy clients: "mixed 7890", or
// "http 7890 · socks 7891".
func portsText(cfg mihomo.Config) string {
	var parts []string
	add := func(name string, port int) {
		if port > 0 {
			parts = append(parts, name+" "+strconv.Itoa(port))
		}
	}
	add("mixed", cfg.MixedPort)
	add("http", cfg.Port)
	add("socks", cfg.SocksPort)
	return strings.Join(parts, " · ")
}

// --- Tailscale ---

func (a *app) tailscaleCard(c *ui.Context, s status.Snapshot) ui.Element {
	return ovCard(c, i18n.T.Tailscale, "", func() { a.tailscaleMenu(c, s) }, func() { a.tailscaleSummary(c, s) })
}

// tailscaleMenu is the Tailscale card's 操作: the devices page and the
// tailnet's admin console.
func (a *app) tailscaleMenu(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	pushFace(c, ui.MenuButton(c.Key("ts-actions"), T.Actions, func(m *ui.Menu) {
		if m.Item(T.OvDevices).Chosen() {
			a.openPage(pageTailscale)
		}
		if m.Item(T.OvAdminConsole).Chosen() {
			_ = mygo.Shell.OpenExternal(tailscaleAdminURL)
		}
	}))
}

// tailscaleAdminURL is the tailnet's machines page, where a node key is
// renewed.
const tailscaleAdminURL = "https://login.tailscale.com/admin/machines"

func (a *app) tailscaleSummary(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	ts := s.Tailscale
	st := ts.State
	if !ts.Reachable {
		kv(c, T.State, T.Unreachable)
		unreachableLine(c, svcTailscale, ts.Err)
		kv(c, T.Install, T.InstallKind(string(ts.Install.Kind)))
		return
	}
	if needsLogin(s) {
		// Logged out: the node's addresses and devices are not its own
		// now; the way back in is the one action.
		kvWith(c, T.State, func() {
			ui.Text(c, T.TSState(st.BackendState)).Grow(1).SingleLine()
			a.tsLoginButton(c, s)
		})
		return
	}
	kvWith(c, T.State, func() {
		ui.Text(c, T.TSState(st.BackendState)).SingleLine().Shrink(0)
		if st.Version != "" {
			ovMono(c, shortTSVersion(st.Version)).TextColor(t.TextMuted).SingleLine().Shrink(1)
		}
	})
	if st.Tailnet != "" {
		kvWith(c, T.OvTailnet, func() { ovMono(c, st.Tailnet).SingleLine().Shrink(1).Selectable() })
	}
	kvWith(c, T.OvThisDevice, func() {
		ui.Text(c, st.Self.Name).SingleLine().Shrink(1).Selectable()
		if ip := st.Self.IPv4(); ip.IsValid() {
			ovMono(c, ip.String()).TextColor(t.TextMuted).SingleLine().Shrink(0).Selectable()
		}
	})
	kvTipWith(c, T.ExitNode, T.InfoExitNode, nil, func() { kvControl(a.exitNodePicker(c, s)) })
	a.controlError(c, pendExit)
	kvWith(c, T.MagicDNS, func() {
		ui.Text(c, onOff(st.Prefs.MagicDNS)).SingleLine().Shrink(0)
		if st.Prefs.MagicDNS && st.MagicDNSSuffix != "" {
			ovMono(c, st.MagicDNSSuffix).TextColor(t.TextMuted).SingleLine().Shrink(1)
		}
	})
	if st.KeyExpiry != nil {
		exp := st.KeyExpiry.Local().Format("2006-01-02") + " (" + status.Until(s.Time, *st.KeyExpiry) + ")"
		kvWith(c, T.KeyExpiry, func() {
			e := num(ui.Text(c, exp)).SingleLine().Shrink(1)
			if st.KeyExpiresWithin(s.Time, status.KeyExpiryWarning) {
				e.TextColor(pal(t).warnText)
				ui.Link(c, T.KeyRenew, tailscaleAdminURL).FontSize(plat.calloutSize)
			}
		})
	}
	if p := ts.Process; p != nil {
		processRows(c, p, s.Time)
	}
	kvWith(c, T.Install, func() { // wraps: long in some languages, and the card is half wide
		ui.Text(c, T.InstallKind(string(ts.Install.Kind))+sourceSuffix(ts.Install.Source)).Grow(1).Wrap().LineHeight(lineText).Selectable()
	})
}

// shortTSVersion drops the build suffix of Tailscale's version
// (1.90.6-t1234abcd-g5678 → 1.90.6).
func shortTSVersion(v string) string {
	if i := strings.IndexByte(v, '-'); i > 0 {
		return v[:i]
	}
	return v
}

// --- duet ---

// duetCard is duet's own state: the helper, the takeover, the last apply
// and the health checks, each with the one action that moves it on.
func (a *app) duetCard(c *ui.Context, s status.Snapshot) ui.Element {
	T := i18n.T
	return ovCard(c, "duet", "", func() {
		if pushButton(c.Key("health-rerun"), T.OvHealthRerun).Clicked() {
			a.rerunHealth()
		}
	}, func() { a.duetSummary(c, s) })
}

func (a *app) duetSummary(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	h := s.Helper
	kvWith(c, T.Helper, func() {
		ui.Text(c, h.State.Text()).SingleLine().Shrink(0)
		switch h.State {
		case status.HelperConnected:
			if h.Status != nil && h.Status.HelperVersion != "" {
				ovMono(c, fmt.Sprintf(T.OvHelperVersion, strings.TrimPrefix(h.Status.HelperVersion, "v"))).
					TextColor(t.TextMuted).SingleLine().Shrink(1)
			}
		case status.HelperNotInstalled:
			ui.Spacer(c)
			if pushButton(c.Key("helper-install"), T.FixInstallHelper).Disabled(!s.WritesAllowed).Clicked() {
				a.openPage(pageInstall)
			}
		case status.HelperNotRunning, status.HelperDenied:
			ui.Spacer(c)
			if fileExists(a.helperLogFile()) && pushButton(c.Key("helper-log"), T.HelperViewLog).Clicked() {
				a.openHelperLog()
			}
			if busyButton(c.Key("helper-restart"), T.HelperRestart, false, a.hm.running == T.HelperRestart, !a.helperOpsEnabled(s)).Clicked() {
				a.restartHelper()
			}
		}
	})
	if a.hm.errMsg != "" && h.State != status.HelperConnected {
		errorNote(c, a.hm.errMsg)
	}
	if h.Connected() && h.Status != nil {
		taken := s.TakenOver()
		kvWith(c, T.OvTakeover, func() {
			if taken {
				ui.Text(c, T.OvTakenOver).SingleLine()
				return
			}
			ui.Text(c, T.OvNotTakenOver).TextColor(t.TextMuted).SingleLine()
			ui.Spacer(c)
			if pushButton(c.Key("takeover"), T.PageTakeover).Clicked() {
				a.openPage(pageTakeover)
			}
		})
		kvWith(c, T.LastApply, func() {
			la := h.Status.LastApply
			if la == nil {
				ui.Text(c, T.OvNeverApplied).TextColor(t.TextMuted).SingleLine()
				return
			}
			when := ui.Text(c, status.Ago(s.Time, la.Time)).SingleLine().Shrink(1).Tooltip(la.Time.Local().Format("2006-01-02 15:04:05"))
			if la.Error != "" || la.RolledBack {
				when.TextColor(pal(t).badText)
				ui.Text(c, T.OvApplyFailed).TextColor(pal(t).badText).SingleLine()
			}
		})
	}
	kvWith(c, T.OvHealth, func() {
		rep := s.Health.Report
		if rep == nil {
			ui.Text(c, T.HealthRunning).TextColor(t.TextMuted).SingleLine().Grow(1)
		} else {
			_, lvl := s.HealthBadge()
			e := ui.Text(c, healthShort(rep.Counts())).SingleLine().Shrink(1).Grow(1)
			if lvl != status.LevelOK {
				e.TextColor(levelText(t, lvl))
			}
		}
		if pushButton(c.Key("health-open"), T.FindingsView).Clicked() {
			a.openPage(pageHealth)
		}
	})
}

// healthShort is the report's counts for the row labelled 健康检查: the
// failures, else the warnings, else 全部通过.
func healthShort(cnt map[health.Status]int) string {
	T := i18n.T
	switch {
	case cnt[health.StatusFail] > 0:
		return fmt.Sprintf(T.OvHealthFail, cnt[health.StatusFail])
	case cnt[health.StatusWarn] > 0:
		return fmt.Sprintf(T.OvHealthWarn, cnt[health.StatusWarn])
	case cnt[health.StatusOK] > 0:
		return T.OvHealthOK
	}
	return T.HealthSkip
}

// --- 最近活动 ---

// ovRecentN is how many of duet's events the overview lists.
const ovRecentN = 3

// recentCard lists duet's last few events (as 日志 → duet words them);
// 全部日志 opens that tab.
func (a *app) recentCard(c *ui.Context, s status.Snapshot) ui.Element {
	T := i18n.T
	return ovCard(c, T.OvRecent, "", func() {
		if pushButton(c.Key("recent-all"), T.OvRecentAll).Clicked() {
			a.showDuetLog()
		}
	}, func() { a.recentEvents(c, s) })
}

func (a *app) recentEvents(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	rows := recentRows(a.events.evs, a.eventName, ovRecentN)
	if len(rows) == 0 {
		ui.Text(c, T.EventsEmpty).TextColor(t.TextMuted).SingleLine().MinHeight(plat.rowMinHeight)
		return
	}
	for i, it := range rows {
		e, r := it.e, it.r
		if it.n > 1 {
			r.text = fmt.Sprintf("%s ×%d", r.text, it.n)
		}
		el := ui.Row(c.Key(fmt.Sprint("ev", i))).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Label(r.kind + " " + r.text)
		if r.page != "" {
			el.Role(ui.RoleButton).Focusable()
		}
		el.Children(func() {
			ovMono(c, eventTime(s.Time, e.Time)).FontSize(plat.smallSize).TextColor(t.TextMuted).Shrink(0).Width(40)
			tag(c, r.kind)
			txt := ui.Text(c, r.text).SingleLine().Shrink(1).MinWidth(0).Tooltip(r.text)
			if r.failed {
				txt.TextColor(pal(t).badText)
			}
		})
		if el.Clicked() && r.page != "" {
			if r.hist != "" {
				a.showHistoryEntry(r.hist)
			} else {
				a.openPage(r.page)
			}
		}
	}
}

// recentRow is one row of 最近活动: the newest of n events in a row that
// read the same (the periodic health runs).
type recentRow struct {
	e rpc.Event
	r eventRow
	n int
}

// recentRows are the last max events in words, newest first, a run of
// events that read the same folded into one row with its count.
func recentRows(evs []rpc.Event, name nameOf, limit int) []recentRow {
	var rows []recentRow
	for i := len(evs) - 1; i >= 0; i-- {
		r, ok := describeEvent(evs[i], name)
		if !ok {
			continue
		}
		if n := len(rows); n > 0 && rows[n-1].r.kind == r.kind && rows[n-1].r.text == r.text {
			rows[n-1].n++
			continue
		}
		if len(rows) == limit {
			break
		}
		rows = append(rows, recentRow{evs[i], r, 1})
	}
	return rows
}

// eventTime is an event's time in the list: the clock for today, the
// date before.
func eventTime(now, at time.Time) string {
	at, now = at.Local(), now.Local()
	if at.Format(time.DateOnly) == now.Format(time.DateOnly) {
		return at.Format("15:04")
	}
	return at.Format("01-02")
}
