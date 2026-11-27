package main

import (
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
)

// duetPair is the signature: the two networks side by side, each with its
// state mark, and the helper under them. State is told by shape as well
// as color (Differentiate Without Color, and the owner's rule for the
// tray icon): a filled dot is running, a ring with a dot in it needs
// attention, an empty ring is down or unknown. Each line carries its
// state in words as a tooltip and for VoiceOver.
func (a *app) duetPair(c *ui.Context, s status.Snapshot, padX float32) {
	t := c.Theme()
	T := i18n.T
	a.windowKeys(c)
	mstate, mtext := markDown, T.Unreachable
	if s.Mihomo.Reachable {
		mstate, mtext = markOK, T.Running
	}
	ui.Column(c).Gap(6).Padding(8, padX).Children(func() {
		pair := func(key, name, text string, m stateMark, col ui.Color) {
			label := i18n.F(i18n.T.StateLabel, name, text)
			ui.Row(c.Key(key)).Gap(6).AlignItems(ui.Center).Tooltip(label).Label(label).Role(ui.RoleStatus).Children(func() {
				stateDot(c, m, col, 8)
				ui.Text(c, name).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine()
			})
		}
		ui.Row(c).Gap(14).Children(func() {
			pair("m", T.Mihomo, mtext, mstate, stateMarkColor(t, mstate))
			tm := tsMark(s)
			ttext := T.Unreachable
			if s.Tailscale.Reachable {
				ttext = T.TSState(s.Tailscale.State.BackendState)
			}
			pair("t", T.Tailscale, ttext, tm, tsColor(t, s))
		})
		hm := markDown
		hcol := t.TextMuted
		switch s.Helper.State {
		case status.HelperConnected:
			hm, hcol = markOK, t.Success
		case status.HelperNotRunning, status.HelperDenied, status.HelperNotInstalled:
			hm, hcol = markAttention, t.Warning
		}
		if helperOutdated(s) {
			// The version handshake is one row here: the state, and 更新.
			ui.Row(c.Key("h-update")).Gap(6).AlignItems(ui.Center).Children(func() {
				pair("h", T.HelperNeedsUpdate, T.HelperNeedsUpdate, markAttention, t.Warning)
				ui.Spacer(c)
				a.helperUpdateButton(c, s, "side-update")
			})
		} else {
			pair("h", T.Helper+" · "+s.Helper.State.Text(), s.Helper.State.Text(), hm, hcol)
		}
		if !s.WritesAllowed {
			// Read-only is a state of the whole app: one line here, on
			// every page, instead of a banner on each; the tooltip says
			// how to turn it off.
			ui.Row(c.Key("ro")).Gap(6).AlignItems(ui.Center).Tooltip(T.WritesDisabledHint).
				Label(T.ReadOnlyShort + "：" + T.WritesDisabledHint).Role(ui.RoleStatus).Children(func() {
				ui.Icon(c, iconSecure).FontSize(iconSmall).TextColor(t.TextMuted).MarginStart(-2).Shrink(0)
				ui.Text(c, T.ReadOnlyShort).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine()
			})
		}
	})
}

// windowKeys are the main window's standard shortcuts that belong to no
// one control: ⌘, opens 设置, as in every Mac app. Built with the
// sidebar, so they work on every page.
func (a *app) windowKeys(c *ui.Context) {
	if a.shotMode {
		// The hidden shot window gets no display frames to run motion
		// in: show every change at once, so captures show the end state.
		// The sidebar is built before the page, so this covers it.
		c.DisableAnimations()
	}
	c.OnWindowShortcut(ui.Cmd, ui.KeyComma, func() {
		if a.page != pageSettings {
			old := a.page
			a.page = pageSettings
			a.pageChanged(old)
		}
	})
}

// stateMark is the shape of a state: the same three shapes everywhere
// state is shown in the window.
type stateMark int

const (
	markOK        stateMark = iota // filled dot
	markAttention                  // ring with a dot in it
	markDown                       // empty ring
)

func stateMarkColor(t *ui.Theme, m stateMark) ui.Color {
	switch m {
	case markOK:
		return t.Success
	case markAttention:
		return t.Warning
	}
	return t.Danger
}

// stateDot draws a state mark of the given size.
func stateDot(c *ui.Context, m stateMark, col ui.Color, size float32) ui.Element {
	switch m {
	case markOK:
		return dot(c, col, size)
	case markAttention:
		return ui.Box(c).Size(size, size).Radius(size/2).Border(1.5, col).Shrink(0).
			AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Box(c).Size(size/2-1, size/2-1).Radius(size / 4).Background(col)
		})
	}
	return ui.Box(c).Size(size, size).Radius(size/2).Border(1.5, col).Shrink(0)
}

// tsMark is the shape of Tailscale's state (tsColor is its color).
func tsMark(s status.Snapshot) stateMark {
	ts := s.Tailscale
	switch {
	case !ts.Reachable:
		return markDown
	case ts.State.Running():
		return markOK
	case ts.State.BackendState == "Stopped" || ts.State.BackendState == "Starting":
		return markAttention
	}
	return markDown
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

// levelMark is the shape of an overall status level: as the sidebar's,
// warnings are a ring with a dot, errors and the unknown an empty ring
// (the words beside the mark tell those two apart).
func levelMark(l status.Level) stateMark {
	switch l {
	case status.LevelOK:
		return markOK
	case status.LevelWarn:
		return markAttention
	}
	return markDown
}
