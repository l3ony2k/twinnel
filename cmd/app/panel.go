package main

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// panelView is the flyout under the tray icon: a popover over the menu
// bar's own material on macOS, laid out like Control Center's modules and
// the Wi-Fi menu: the overall state and the rates on top, what needs
// attention (only when something does), one rounded module per network
// with its state in the header, compact rows for the daily controls, and
// menu rows at the foot.
//
// Geometry (one grid, every row on it): the flyout pads its content by
// panelInset; a module pads by moduleInset and its rows by rowInset, so
// every row's leading column starts panelInset+moduleInset+rowInset from
// the edge, outside a module too (rowInset+moduleInset there).
func (a *app) panelView(c *ui.Context) {
	a.applyTheme(c)
	// Text one step under the main window's on Windows and Linux (13 for
	// 14), as on macOS: the size of Control Center's and the Wi-Fi menu's.
	th := *c.Theme()
	th.FontSize = plat.panelTextSize
	c.SetTheme(&th)
	t := c.Theme()
	T := i18n.T
	s := a.mon.Snapshot()
	if a.shotSnap != nil {
		a.shotSnap(&s)
	}
	c.Root().Background(ui.Transparent)
	// As wide as the flyout, as tall as its content: the flyout fits it
	// (FitContent); a height of its own would keep the flyout at that.
	box := ui.Column(c).FillWidth().Shrink(0)
	switch {
	case c.Vibrancy(): // over the popover's material on macOS
	case runtime.GOOS == "windows":
		box.Background(t.Background)
	default:
		box.Background(t.Background).Radius(plat.groupRadius+2).Border(1, pal(t).groupLine)
	}
	box.Padding(panelInset).Gap(moduleGap).Children(func() {
		a.panelHeader(c, s)
		a.panelAttention(c, s)
		panelModule(c, func() { a.panelMihomo(c, s) })
		panelModule(c, func() { a.panelTailscale(c, s) })
		if a.actionErr != "" {
			ui.Column(c).PaddingX(rowInset + moduleInset).Children(func() { errorNote(c, a.actionErr) })
		}
		a.fitShotPanel(box)
		// The foot as a menu bar extra ends: a hairline, then menu rows.
		ui.Column(c).Children(func() {
			ui.Box(c).Height(1).Background(pal(t).separator).MarginX(rowInset + moduleInset).MarginY(2)
			if h := s.Helper.State; h != status.HelperNotInstalled && h != status.HelperUnknown {
				// 应急恢复: works while things are broken (statusui.go).
				r := panelMenuRow(c.Key("recovery"), T.Recovery, false, func(m *ui.Menu) { a.recoveryItems(m, s) })
				r.Children(func() {
					ui.Row(c).Gap(4).AlignItems(ui.Center).Grow(1).Children(func() {
						ui.Text(c, T.Recovery).SingleLine()
						infoTip(c, "", T.InfoRecovery, nil)
					})
					ui.Icon(c, iconCollapsed).FontSize(iconSmall).TextColor(t.TextMuted)
				})
			}
			footRow(c.Key("open"), T.PanelOpen, a.openMain)
			footRow(c.Key("quit"), T.PanelQuit, mygo.App.Quit)
		})
	})
}

// The flyout's grid (see panelView).
const (
	panelInset  = 10 // the flyout's padding
	moduleInset = 4  // a module's padding
	rowInset    = 8  // a row's own padding
	moduleGap   = 10 // between modules
	panelRowH   = 28 // a compact row, as a menu item
	panelHeadH  = 32 // a module's header row
	leadCol     = 24 // the leading column: a state mark, an icon, the TUN toggle
	moduleRad   = 12 // a module's corners; a row's hover fill is moduleRad-moduleInset
)

// panelModule is one rounded module on the flyout's material, as Control
// Center's: a quiet fill a step off the material, no border.
func panelModule(c *ui.Context, body func()) {
	t := c.Theme()
	fill := ui.RGBA(0, 0, 0, 0.045)
	if t.Dark {
		fill = ui.RGBA(255, 255, 255, 0.06)
	}
	ui.Column(c).Padding(moduleInset).Radius(moduleRad).Background(fill).Shrink(0).Children(body)
}

// panelRow is a row of the flyout on its grid: the leading column, the
// row's height, the hover fill in the module's inner radius. The caller
// adds its children (and its Menu, or its click).
func panelRow(c *ui.Context, label string) ui.Element {
	t := c.Theme()
	row := ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(rowInset).MinHeight(panelRowH).
		Radius(moduleRad - moduleInset).Label(label).Focusable()
	if (row.Hovered() || row.Focused()) && !row.IsDisabled() {
		row.Background(t.Text.Alpha(0.08))
	}
	return row
}

// panelMenuRow is a panelRow that opens a native menu just below it: a
// host carries the menu (menuBelow) and the row, which the caller fills,
// shows the host's hover and focus. disabled turns the menu off.
func panelMenuRow(c *ui.Context, label string, disabled bool, items func(m *ui.Menu)) ui.Element {
	t := c.Theme()
	host := menuBelow(ui.Column(c).Label(label).Disabled(disabled).FocusRing(false).Menu(items))
	var row ui.Element
	host.Children(func() {
		row = ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(rowInset).MinHeight(panelRowH).
			Radius(moduleRad - moduleInset)
		if (host.Hovered() || host.Focused()) && !host.IsDisabled() {
			row.Background(t.Text.Alpha(0.08))
		}
	})
	return row
}

// footRow is a row of the flyout's foot, drawn as a menu item.
func footRow(c *ui.Context, label string, fn func()) {
	row := panelRow(c, label).Role(ui.RoleButton)
	row.Children(func() { ui.Text(c, label).Grow(1).SingleLine() })
	if row.Clicked() {
		fn()
	}
}

// leading is the row's leading column with an icon in it.
func leading(c *ui.Context, icon *ui.SVG, col ui.Color) {
	ui.Row(c).Width(leadCol).Justify(ui.Center).Shrink(0).Children(func() {
		ui.Icon(c, icon).FontSize(iconNav).TextColor(col)
	})
}

// panelHeader is the overall state and, while mihomo answers, the rates.
func (a *app) panelHeader(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	// Each stays whole; the rates move under the state when a language's
	// state leaves them no room. Without rates (mihomo down) the state is
	// the whole row, with no room kept for a second line (kubai R-19).
	row := ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(rowInset + moduleInset).MinHeight(panelHeadH)
	if s.Mihomo.Reachable {
		row.Wrap()
	}
	row.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Children(func() {
			ui.Row(c).Width(leadCol).Justify(ui.Center).Shrink(0).Children(func() {
				stateDot(c, levelMark(s.Level), levelColor(t, s.Level), 10)
			})
			ui.Text(c, s.Level.Text()).FontWeight(600).NoWrap()
		})
		if s.Mihomo.Reachable {
			m := s.Mihomo.Traffic
			num(small(c, "↓ "+status.Rate(m.Down)+"  ↑ "+status.Rate(m.Up))).NoWrap()
		}
	})
}

// panelAttention lists what needs attention, a compact row each, in a
// module of its own; nothing at all when nothing does. A state a network's
// module already shows in its header (down, logged out, stopped) is not
// repeated here.
func (a *app) panelAttention(c *ui.Context, s status.Snapshot) {
	var reasons []status.Reason
	for _, r := range s.Reasons {
		switch {
		case r.Health && a.healthSrc != nil: // the health row says it
		case r.Code == status.ReasonMihomoDown, r.Code == status.ReasonTSDown,
			r.Code == status.ReasonTSNeedsLogin, r.Code == status.ReasonTSStopped:
		default:
			reasons = append(reasons, r)
		}
	}
	health := a.healthAttention(s)
	helper := s.Helper.State != status.HelperConnected && s.Helper.State != status.HelperUnknown
	if len(reasons) == 0 && !health && !helper {
		return
	}
	t := c.Theme()
	// The worst first: a fault before a warning (R-unslop-34).
	sort.SliceStable(reasons, func(i, j int) bool { return reasons[i].Level > reasons[j].Level })
	panelModule(c, func() {
		for i, r := range reasons {
			if i == 3 {
				ui.Row(c).PaddingX(rowInset).MinHeight(panelRowH).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Width(leadCol + 8).Shrink(0)
					small(c, fmt.Sprintf("… +%d", len(reasons)-3))
				})
				break
			}
			// The icon's shape tells a warning from a fault, in the
			// level's own color, the same as the health row's (one
			// warning color in the module: R-unslop-15, kubai R-6); the
			// text stays in the text color.
			ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(rowInset).PaddingY(5).MinHeight(panelRowH).Children(func() {
				leading(c, levelIcon(r.Level), levelColor(t, r.Level))
				ui.Text(c, r.Text).Grow(1).Shrink(1).Wrap().LineHeight(lineText)
			})
		}
		if health {
			a.healthLine(c, s)
		}
		if helper {
			a.helperLine(c, s)
		}
	})
}

// healthAttention: the health checks found something (the health row
// shows); all passed or not yet run shows nothing.
func (a *app) healthAttention(s status.Snapshot) bool {
	if a.healthSrc == nil || s.Health.Report == nil {
		return false
	}
	n, _ := s.HealthBadge()
	return n > 0
}

// healthLine is the flyout's row about the health checks' findings;
// clicking it opens the health page.
func (a *app) healthLine(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	// The one number (status.Snapshot.HealthBadge): the badge, the
	// overview and the health page show the same.
	n, lvl := s.HealthBadge()
	_, fail := s.Health.Counts()
	icon, col, text := iconWarning, levelColor(t, status.LevelWarn), i18n.F(T.PanelHealthWarn, n)
	if fail > 0 {
		icon, col, text = iconError, levelColor(t, lvl), fmt.Sprintf(T.PanelHealthFail, n)
		if lvl == status.LevelWarn {
			icon = iconWarning // before takeover: findings, not faults
		}
	}
	row := panelRow(c.Key("health-line"), text).Role(ui.RoleButton)
	row.Children(func() {
		leading(c, icon, col)
		ui.Text(c, text).Grow(1).Shrink(1).SingleLine()
		switch age := healthAge(s); {
		case s.Health.Running:
			ui.Spinner(c).Size(12, 12) // the number is being updated
		case age != "":
			num(small(c, age)).SingleLine()
		}
		ui.Icon(c, iconCollapsed).FontSize(iconSmall).TextColor(t.TextMuted)
	})
	if row.Clicked() {
		old := a.page
		a.page = pageHealth
		a.openMain()
		a.pageChanged(old)
	}
}

// helperLine is the flyout's row about the helper while it is not
// connected: installed but down (a warning: what needs it is locked), or
// not installed (quiet). A click opens where that is dealt with.
func (a *app) helperLine(c *ui.Context, s status.Snapshot) {
	h := s.Helper
	t := c.Theme()
	T := i18n.T
	icon, col, text := iconSkip, t.TextMuted, T.Helper+T.LabelSep+h.State.Text()
	if h.State != status.HelperNotInstalled {
		icon, col, text = iconWarning, t.Warning, a.helperDownText(s) // helpergroup.go
	}
	row := panelRow(c.Key("helper-line"), text).Role(ui.RoleButton)
	row.Children(func() {
		leading(c, icon, col)
		ui.Text(c, text).Grow(1).Shrink(1).SingleLine()
		ui.Icon(c, iconCollapsed).FontSize(iconSmall).TextColor(t.TextMuted)
	})
	if row.Clicked() {
		a.openHelperSetup(s)
	}
}

// moduleHeader is a module's first row: its state mark in the leading
// column, its name, a one-line summary, then its own control (trailing).
func moduleHeader(c *ui.Context, m stateMark, col ui.Color, name, summary, detail string, trailing func()) {
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(rowInset).MinHeight(panelHeadH).Children(func() {
		ui.Row(c).Width(leadCol).Justify(ui.Center).Shrink(0).Children(func() { stateDot(c, m, col, 8) })
		ui.Text(c, name).FontWeight(600).NoWrap()
		e := ui.Text(c, summary).TextColor(t.TextMuted).Grow(1).Shrink(1).TextAlign(ui.End).SingleLine()
		if detail != "" {
			e.Tooltip(detail)
		}
		if trailing != nil {
			trailing()
		}
	})
}

func (a *app) panelMihomo(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	m := s.Mihomo
	if !m.Reachable {
		// Down: no stale values, the plain state (the error on hover), and
		// 启动 when the helper can start it (else the lock says why not).
		// Never a dead end (kubai R-19): 启动 when the helper can start
		// it, else 重试 (ask again; its icon turns).
		moduleHeader(c, markDown, t.Danger, T.Mihomo, T.Unreachable, m.Err, func() {
			if !s.WritesAllowed || m.Unauthorized || !s.Helper.Connected() {
				a.panelRetry(c)
				return
			}
			busy := a.hd.busy == T.StartMihomo
			if busyButton(c.Key("start-mihomo"), T.StartShort, false, busy, !s.Can().Helper || a.hd.busy != "").Height(22).Clicked() {
				a.mihomoService(rpc.MethodServiceStart, T.StartMihomo)
			}
			a.helperLock(c, s)
		})
		return
	}
	moduleHeader(c, markOK, t.Success, T.Mihomo, T.Running+versionSuffix(m.Version), "", nil)
	ui.Column(c).PaddingX(rowInset).PaddingY(4).Children(func() {
		a.modeControl(c, s).FillEqually().Height(24).Label(T.Mode)
	})
	a.controlError(c, pendMode)
	a.tunRow(c, s)
	a.controlError(c, pendTun)
	a.keepExitFresh(s)
	if p := m.Primary; p != nil {
		a.nodeRow(c, s, p)
		a.controlError(c, nodeKey(p.Name))
	}
}

// panelRetry is a module header's 重试: a compact push button whose
// refresh icon turns while the monitor asks again (retryButton's timing).
func (a *app) panelRetry(c *ui.Context) {
	now := time.Now()
	spin := a.retry.visible(now)
	b := pushFace(c, ui.Button(c.Key("panel-retry"), "")).Label(i18n.T.Retry).Busy(spin).Height(22).Children(func() {
		turningIcon(c, iconRefresh, spin)
		ui.Text(c, i18n.T.Retry).SingleLine()
	})
	if b.Clicked() && !spin {
		a.retry = busySpan{start: now.Add(-busyDelay), end: now}
		time.AfterFunc(busyMin+10*time.Millisecond, func() { mygo.RunOnMain(a.invalidate) })
		a.mon.Refresh()
	}
}

// tunRow is TUN as Control Center's toggles: a round button, filled in
// the accent with the glyph in white while on, a neutral disc while off;
// the device (or 关闭) beside it says the same in words. The whole row
// toggles.
func (a *app) tunRow(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	on := a.tunOn(s)
	dev := T.TunOff
	if on {
		dev = s.Mihomo.Config.Tun.Device
		if dev == "" {
			dev = T.TunOn
		}
	}
	can := s.Can().Mihomo
	row := panelRow(c.Key("tun"), T.Tun).Role(ui.RoleSwitch).Checked(on).Disabled(!can)
	row.Children(func() {
		disc, glyph := t.Text.Alpha(0.1), t.Text
		if on {
			disc, glyph = t.Accent, t.AccentText
		}
		ui.Row(c).Size(leadCol, leadCol).Radius(leadCol / 2).Background(disc).AlignItems(ui.Center).Justify(ui.Center).Shrink(0).
			Children(func() { ui.Icon(c, iconTun).FontSize(iconInline).TextColor(glyph) })
		ui.Row(c).Gap(4).AlignItems(ui.Center).Grow(1).Children(func() {
			ui.Text(c, T.Tun).SingleLine()
			infoTip(c, "", T.InfoTun, nil)
		})
		ui.Text(c, dev).TextColor(t.TextMuted).SingleLine().Shrink(1)
	})
	if row.Clicked() && can {
		a.setTun(!on)
	}
}

// nodeRow is the primary group's choice as a pop-up row: the group's name,
// its choice and ⌃⌄, a native menu of its members (checked: the choice).
// When the choice is itself a group (自动选择), the node it resolved to
// shows under it with its delay; else the delay sits on the row.
func (a *app) nodeRow(c *ui.Context, s status.Snapshot, p *status.Group) {
	t := c.Theme()
	T := i18n.T
	sel := pendingValue(a, nodeKey(p.Name), p.Now)
	can := s.Can().Mihomo && p.Selectable()
	via := p.Node != "" && p.Node != sel
	delay := func() {
		d := num(ui.Text(c, p.DelayText()).FontSize(plat.calloutSize).TextColor(delayColor(t, p.Delay, p.Tested)).Shrink(0))
		if p.Tested && !p.DelayAt.IsZero() {
			d.Tooltip(status.Ago(s.Time, p.DelayAt)) // how old the figure is
		}
	}
	row := panelMenuRow(c.Key("node"), T.Node, !can, func(m *ui.Menu) {
		for _, mem := range p.Members {
			if m.Item(mem.Name).Checked(mem.Name == sel).Trailing(mem.DelayText()).Chosen() && mem.Name != sel {
				a.selectNode(p.Name, mem.Name)
			}
		}
	})
	row.Children(func() {
		leading(c, iconGlobe, t.TextMuted)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).PaddingY(4).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, p.Name).SingleLine().Shrink(0)
				middleText(c, sel).TextColor(t.TextMuted).Grow(1).TextAlign(ui.End)
				if !via {
					delay()
				}
			})
			if via {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					small(c, p.Node).Grow(1).Shrink(1).MinWidth(0).TextAlign(ui.End).SingleLine().TruncateMiddle().Tooltip(p.Node)
					delay()
				})
			}
		})
		if can {
			ui.Icon(c, iconUpDown).FontSize(iconSmall).TextColor(t.TextMuted)
		}
	})
}

func (a *app) panelTailscale(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	ts := s.Tailscale
	st := ts.State
	summary, detail := T.Unreachable, ts.Err
	if ts.Reachable {
		summary = T.TSState(st.BackendState)
		if st.Running() {
			summary += " · " + fmt.Sprintf(T.PanelPeers, st.OnlinePeers(), len(st.Peers))
		}
	}
	// The header's switch connects and disconnects, as Tailscale's own
	// menu does.
	moduleHeader(c, tsMark(s), tsColor(t, s), T.Tailscale, summary, detail, func() {
		switch {
		case needsLogin(s):
			a.tsLoginButton(c, s)
		case ts.Reachable:
			a.tsPowerSwitch(c, s).Size(32, 18).Radius(9)
		}
	})
	a.controlError(c, pendTS)
	if !ts.Reachable || needsLogin(s) {
		return // its addresses and exit node are not this node's now
	}
	if ip := st.Self.IPv4(); ip.IsValid() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(rowInset).MinHeight(panelRowH).Children(func() {
			leading(c, iconTailscale, t.TextMuted)
			middleText(c, st.Self.Name).Grow(1)
			num(ui.Text(c, ip.String()).TextColor(t.TextMuted).SingleLine().Shrink(0).Selectable())
		})
	}
	a.exitNodeRow(c, s)
	a.controlError(c, pendExit)
}

// exitNodeRow is the exit node as a pop-up row, as nodeRow: 不使用, then
// the peers that offer one. Choosing one while TUN is on asks first
// (chooseExitNode).
func (a *app) exitNodeRow(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	st := s.Tailscale.State
	sel := T.ExitNodeNone
	if st.ExitNode != nil {
		sel = st.ExitNode.Name
	}
	sel = pendingValue(a, pendExit, sel)
	opts := exitNodeOptions(st)
	can := s.Can().Tailscale && len(opts) > 1
	row := panelMenuRow(c.Key("exit-node"), T.ExitNode, !can, func(m *ui.Menu) {
		for i, name := range opts {
			if i == 1 {
				m.Separator()
			}
			if m.Item(name).Checked(name == sel).Chosen() && name != sel {
				a.chooseExitNode(st, name, s.Mihomo.Reachable && s.Mihomo.Config.Tun.Enable)
			}
		}
	})
	row.Children(func() {
		col := t.TextMuted
		if sel != T.ExitNodeNone {
			col = t.Text
		}
		leading(c, iconExitNode, col)
		ui.Text(c, T.ExitNode).SingleLine().Shrink(0)
		infoTip(c, "", T.InfoExitNode, nil)
		ui.Text(c, sel).TextColor(t.TextMuted).Grow(1).Shrink(1).TextAlign(ui.End).SingleLine()
		if can {
			ui.Icon(c, iconUpDown).FontSize(iconSmall).TextColor(t.TextMuted)
		}
	})
}

func versionSuffix(v string) string {
	if v == "" {
		return ""
	}
	return " · " + v
}

// needsLogin: Tailscale answers and wants a login.
func needsLogin(s status.Snapshot) bool {
	st := s.Tailscale.State.BackendState
	return s.Tailscale.Reachable && (st == "NeedsLogin" || st == "NeedsMachineAuth")
}

// fitShotPanel sizes shot mode's stand-in for the flyout (a hidden window)
// and the Linux panel (a window, panel_linux.go), which do not fit their
// content, to the content's height, as the live flyout's FitContent does,
// so neither shows empty space under it.
func (a *app) fitShotPanel(box ui.Element) {
	if !(a.shotMode || plat.panelWindow) || a.panel == nil {
		return // a flyout fits its content itself (FitContent)
	}
	h := int(math.Ceil(float64(box.LayoutBounds().H)))
	if h <= 0 || h == a.panelShotH {
		return
	}
	a.panelShotH = h
	p := a.panel
	mygo.RunOnMain(func() {
		// SetSize sizes the frame: add what the title bar takes.
		_, wh := p.Size()
		_, ch := p.ContentSize()
		p.SetSize(panelWidth, h+max(0, wh-ch))
	})
}
