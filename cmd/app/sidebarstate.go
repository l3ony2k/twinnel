package main

import (
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
)

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
