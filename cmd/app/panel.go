package main

import (
	"context"
	"fmt"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/status"
)

// panelView is the flyout under the tray icon: a popover over the menu
// bar's own material on macOS, laid out like the system's menu bar extras
// (state on top, one section per network, the window and quit at the foot).
func (a *app) panelView(c *ui.Context) {
	a.applyTheme(c)
	t := c.Theme()
	T := i18n.T
	s := a.mon.Snapshot()
	c.Root().Background(ui.Transparent)
	box := ui.Column(c).Fill()
	switch {
	case c.Vibrancy(): // over the popover's material on macOS
	case runtime.GOOS == "windows":
		box.Background(t.Background)
	default:
		box.Background(t.Background).Radius(plat.groupRadius+2).Border(1, pal(t).groupLine)
	}
	sep := func() { ui.Box(c).Height(1).Background(pal(t).separator).MarginY(2) }
	box.Padding(plat.panelPad).Gap(10).Children(func() {
		// Overall state and the live rates.
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			dot(c, levelColor(t, s.Level), 10)
			ui.Text(c, s.Level.Text()).FontSize(15).FontWeight(600)
			ui.Spacer(c)
			if s.Mihomo.Reachable {
				num(small(c, "↓ "+status.Rate(s.Mihomo.Traffic.Down)+"   ↑ "+status.Rate(s.Mihomo.Traffic.Up)))
			}
		})
		reasons := s.Reasons
		if a.healthSrc != nil { // the health line below says it
			reasons = nil
			for _, r := range s.Reasons {
				if !r.Health {
					reasons = append(reasons, r)
				}
			}
		}
		for i, r := range reasons {
			if i == 3 {
				small(c, fmt.Sprintf("… +%d", len(reasons)-3))
				break
			}
			ui.Text(c, r.Text).FontSize(plat.bodySize - 1).TextColor(levelText(t, r.Level)).Wrap()
		}
		a.healthLine(c, s)
		a.helperLine(c, s)
		sep()
		a.panelMihomo(c, s)
		sep()
		a.panelTailscale(c, s)
		sep()
		if !s.WritesAllowed {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconSecure).FontSize(12).TextColor(t.TextMuted)
				small(c, T.ReadOnlyShort).Grow(1).Wrap()
			})
		}
		if a.actionErr != "" {
			errorNote(c, a.actionErr)
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c, T.OpenMainWindow).OnClick(a.openMain)
			ui.Spacer(c)
			ui.Button(c, T.Quit).OnClick(mygo.App.Quit)
		})
	})
}

func (a *app) panelMihomo(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	m := s.Mihomo
	col, state := t.Danger, T.Unreachable
	if m.Reachable {
		col, state = t.Success, T.Running
	}
	sectionTitle(c, T.Mihomo, col, state+versionSuffix(m.Version))
	if !m.Reachable {
		if m.Err != "" {
			small(c, m.Err).Wrap()
		}
		return
	}
	w := s.WritesAllowed
	kvWith(c, T.Mode, func() {
		idx := 0
		labels := make([]string, len(mihomo.Modes))
		for i, md := range mihomo.Modes {
			labels[i] = T.ModeName(string(md))
			if md == m.Config.Mode {
				idx = i
			}
		}
		if ui.Segmented(c.Key("mode"), &idx, labels...).Disabled(!w).Grow(1).Changed() {
			md := mihomo.Modes[idx]
			a.do(func(ctx context.Context) error { return a.mh.SetMode(ctx, md) })
		}
	})
	tun := T.TunOff
	if m.Config.Tun.Enable {
		tun = m.Config.Tun.Device
	}
	kvWith(c, T.Tun, func() {
		a.tunSwitch(c, s)
		ui.Icon(c, iconTun).FontSize(13).TextColor(t.TextMuted)
		small(c, tun).SingleLine().Shrink(1)
	})
	if p := m.Primary; p != nil {
		kvWith(c, p.Name, func() {
			sel := p.Now
			opts := make([]string, len(p.Members))
			for i, mem := range p.Members {
				opts[i] = mem.Name
			}
			if ui.Select(c.Key("node"), &sel, opts).Disabled(!w || !p.Selectable()).Grow(1).Label(T.Node).Changed() {
				group := p.Name
				a.do(func(ctx context.Context) error { return a.mh.SelectProxy(ctx, group, sel) })
			}
		})
		if len(p.Chain) > 0 {
			kvWith(c, T.Exit, func() {
				ui.Text(c, p.Node).SingleLine().Shrink(1)
				ui.Spacer(c)
				num(ui.Text(c, status.DelayText(p.Delay, p.Tested)).FontSize(plat.bodySize - 1).TextColor(delayColor(t, p.Delay, p.Tested)).Shrink(0))
			})
		}
	}
}

func (a *app) panelTailscale(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	ts := s.Tailscale
	st := ts.State
	// The heading's switch connects and disconnects, as Tailscale's own
	// menu does.
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		dot(c, tsColor(t, s), 8)
		ui.Text(c, T.Tailscale).FontWeight(plat.headingWeight)
		ui.Spacer(c)
		state := T.Unreachable
		if ts.Reachable {
			state = T.TSState(st.BackendState)
		}
		small(c, state)
		if ts.Reachable {
			a.tsPowerSwitch(c, s)
		}
	})
	if !ts.Reachable {
		if ts.Err != "" {
			small(c, ts.Err).Wrap()
		}
		return
	}
	if ip := st.Self.IPv4(); ip.IsValid() {
		kv(c, T.SelfIP, ip.String()+" · "+st.Self.Name)
	}
	kv(c, T.OnlinePeers, fmt.Sprintf("%d / %d", st.OnlinePeers(), len(st.Peers)))
	kvWith(c, T.ExitNode, func() { a.exitNodePicker(c, s).Grow(1) })
}

func versionSuffix(v string) string {
	if v == "" {
		return ""
	}
	return " · " + v
}

// helperLine is the panel's line about the helper, while it is not
// connected; clicking it opens the 订阅与覆写 page (first-run view).
func (a *app) helperLine(c *ui.Context, s status.Snapshot) {
	h := s.Helper
	if h.State == status.HelperConnected || h.State == status.HelperUnknown {
		return
	}
	t := c.Theme()
	row := ui.Row(c.Key("helper-line")).Gap(8).AlignItems(ui.Center).Padding(6, 8).MarginX(-8).Radius(plat.controlRadius).Cursor(ui.CursorPointer)
	row.Children(func() {
		ui.Icon(c, iconSkip).FontSize(14).TextColor(t.TextMuted)
		ui.Text(c, i18n.T.Helper+"："+h.State.Text()).Grow(1).SingleLine()
		ui.Icon(c, iconCollapsed).FontSize(12).TextColor(t.TextMuted)
	})
	if row.Hovered() {
		row.Background(t.Text.Alpha(0.08))
	}
	if row.Clicked() {
		old := a.page
		a.page = pageConfig
		a.openMain()
		a.pageChanged(old)
	}
}
