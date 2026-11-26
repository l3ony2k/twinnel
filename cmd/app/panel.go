package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/status"
)

// panelView is the flyout under the tray icon.
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
		box.Background(t.Surface)
	default:
		box.Background(t.Surface).Radius(10).Border(1, t.Border)
	}
	box.Padding(16).Gap(12).Children(func() {
		// Overall status.
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			dot(c, levelColor(t, s.Level), 12)
			ui.Text(c, s.Level.Text()).FontSize(16).Bold()
			ui.Spacer(c)
			if s.Mihomo.Reachable {
				ui.Text(c, status.TrayTitle(s.Mihomo.Traffic)).Font("monospace").FontSize(12).TextColor(t.TextMuted)
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
				ui.Textf(c, "… +%d", len(reasons)-3).FontSize(12).TextColor(t.TextMuted)
				break
			}
			ui.Row(c).Gap(6).AlignItems(ui.Start).Children(func() {
				dot(c, levelColor(t, r.Level), 6).MarginY(5)
				ui.Text(c, r.Text).FontSize(12).Grow(1).Wrap()
			})
		}
		a.healthLine(c, s)
		a.helperLine(c, s)
		ui.Divider(c)
		a.panelMihomo(c, s)
		ui.Divider(c)
		a.panelTailscale(c, s)
		ui.Divider(c)
		if !s.WritesAllowed {
			ui.Text(c, T.WritesDisabledHint).FontSize(11).TextColor(t.TextMuted).Wrap()
		}
		if a.actionErr != "" {
			ui.Text(c, a.actionErr).FontSize(12).TextColor(t.Danger).Wrap()
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.PrimaryButton(c, T.OpenMainWindow).OnClick(a.openMain)
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
			ui.Text(c, m.Err).FontSize(12).TextColor(t.TextMuted).Wrap()
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
		if ui.Segmented(c.Key("mode"), &idx, labels...).Disabled(!w).Changed() {
			md := mihomo.Modes[idx]
			a.do(func(ctx context.Context) error { return a.mh.SetMode(ctx, md) })
		}
	})
	tun := T.TunOff
	if m.Config.Tun.Enable {
		tun = T.TunOn + " · " + m.Config.Tun.Device
	}
	kvWith(c, T.Tun, func() {
		a.tunSwitch(c, s)
		ui.Text(c, tun).FontSize(13).SingleLine().Shrink(1)
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
			ui.Text(c, status.DelayText(p.Delay, p.Tested)).FontSize(12).TextColor(delayColor(t, p.Delay, p.Tested)).Shrink(0)
		})
		if len(p.Chain) > 1 {
			kv(c, T.Exit, strings.Join(p.Chain, " → "))
		}
	}
}

func (a *app) panelTailscale(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	ts := s.Tailscale
	st := ts.State
	col, state := t.Danger, T.Unreachable
	if ts.Reachable {
		state = T.TSState(st.BackendState)
		switch {
		case st.Running():
			col = t.Success
		case st.BackendState == "Stopped" || st.BackendState == "Starting":
			col = t.Warning
		}
	}
	sectionTitle(c, T.Tailscale, col, state)
	if !ts.Reachable {
		if ts.Err != "" {
			ui.Text(c, ts.Err).FontSize(12).TextColor(t.TextMuted).Wrap()
		}
		return
	}
	if ip := st.Self.IPv4(); ip.IsValid() {
		kv(c, T.SelfIP, ip.String()+" · "+st.Self.Name)
	}
	kv(c, T.OnlinePeers, fmt.Sprintf("%d / %d", st.OnlinePeers(), len(st.Peers)))
	kvWith(c, T.ExitNode, func() { a.exitNodePicker(c, s).Grow(1) })
	ui.Row(c).Gap(8).Children(func() { a.tsPowerButton(c, s) })
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
	row := ui.Row(c.Key("helper-line")).Gap(8).AlignItems(ui.Center).Padding(6, 8).Radius(6).Cursor(ui.CursorPointer)
	row.Children(func() {
		ui.Icon(c, iconSkip).FontSize(14).TextColor(t.TextMuted)
		ui.Text(c, i18n.T.Helper+"："+h.State.Text()).FontSize(12).Grow(1).SingleLine()
		ui.Icon(c, iconCollapsed).FontSize(12).TextColor(t.TextMuted)
	})
	if row.Hovered() {
		row.Background(t.Border.Alpha(0.5))
	}
	if row.Clicked() {
		old := a.page
		a.page = pageConfig
		a.openMain()
		a.pageChanged(old)
	}
}
