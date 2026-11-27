package main

import (
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// twinnelPair is the signature: one row of state marks, mihomo, Tailscale
// and the helper, each with its state in words as a tooltip and for
// VoiceOver. State is told by shape as well as color (Differentiate
// Without Color, and the owner's rule for the tray icon): a filled dot is
// running, a ring with a dot in it needs attention, an empty ring is down
// or unknown, a neutral ring with a bar (⊖) is another program's mihomo
// on the controller (mihomoMark). A second row shows only when something
// asks for an action (install the helper, start it, update it).
func (a *app) twinnelPair(c *ui.Context, s status.Snapshot, padX float32) {
	t := c.Theme()
	T := i18n.T
	a.windowKeys(c)
	mstate, mcol, mtext := mihomoMark(t, s.MihomoView())
	pair := func(key, name, text string, m stateMark, col ui.Color) {
		label := i18n.F(i18n.T.StateLabel, name, text)
		ui.Row(c.Key(key)).Gap(navGap).AlignItems(ui.Center).Shrink(1).MinWidth(0).Tooltip(label).Label(label).Role(ui.RoleStatus).Children(func() {
			footMark(c, m, col)
			ui.Text(c, name).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine().Shrink(1)
		})
	}
	hm, hcol := markDown, t.TextMuted
	switch s.Helper.State {
	case status.HelperConnected:
		hm, hcol = markOK, t.Success
	case status.HelperNotRunning, status.HelperDenied, status.HelperNotInstalled:
		hm, hcol = markAttention, t.Warning
	}
	// One line per state, so that no name is ever cut (the sidebar is as
	// wide as these lines need, sidebarNeed). The helper's line is the
	// way to deal with it when it needs something.
	ui.Column(c).Gap(5).Padding(8, padX).Children(func() {
		pair("m", T.Mihomo, mtext, mstate, mcol)
		tm := tsMark(s)
		ttext := T.Unreachable
		if s.Tailscale.Reachable {
			ttext = T.TSState(s.Tailscale.State.BackendState)
		}
		pair("t", T.Tailscale, ttext, tm, tsColor(t, s))
		switch {
		case helperOutdated(s):
			// The version handshake is one row here: the state, and 更新.
			ui.Row(c.Key("h-update")).Gap(6).AlignItems(ui.Center).Children(func() {
				pair("hu", T.HelperNeedsUpdate, T.HelperNeedsUpdate, markAttention, t.Warning)
				ui.Spacer(c)
				a.helperUpdateButton(c, s, "side-update")
			})
		case s.Helper.State == status.HelperNotInstalled, s.Helper.State == status.HelperNotRunning, s.Helper.State == status.HelperDenied:
			text := T.Helper + " · " + s.Helper.State.Text()
			if s.Helper.State != status.HelperNotInstalled {
				text = a.helperDownText(s) // helpergroup.go
			}
			ui.Row(c.Key("h")).Gap(navGap).AlignItems(ui.Center).Children(func() {
				footMark(c, hm, hcol)
				footLink(c.Key("h-act"), text, func() { a.openHelperSetup(s) })
			})
		default:
			pair("h", T.HelperShort, s.Helper.State.Text(), hm, hcol)
		}
	})
}

// footMark is a state mark of the sidebar's foot, centered in a box as
// wide as a nav row's icon, so the marks sit on the icons' column and the
// names start on the labels' line (unslop R-8).
func footMark(c *ui.Context, m stateMark, col ui.Color) {
	_, _, icon := navMetrics()
	ui.Row(c).Width(icon + 2).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
		stateDot(c, m, col, 8)
	})
}

// footLink is the sidebar foot's action row: small text in the accent that
// opens where the state is dealt with (a button: the arrow cursor).
func footLink(c *ui.Context, text string, fn func()) {
	t := c.Theme()
	l := ui.Row(c).Gap(4).AlignItems(ui.Center).Role(ui.RoleButton).Label(text).Focusable()
	l.Children(func() {
		ui.Text(c, text).FontSize(plat.smallSize).TextColor(t.Accent).SingleLine().Shrink(1)
		ui.Icon(c, iconCollapsed).FontSize(iconSmall).TextColor(t.Accent)
	})
	if l.Clicked() {
		fn()
	}
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
	// markForeign: a ring with a bar across (⊖), neutral: another
	// program's mihomo holds the controller (mihomoMark).
	markForeign
)

func stateMarkColor(t *ui.Theme, m stateMark) ui.Color {
	switch m {
	case markOK:
		return t.Success
	case markAttention:
		return t.Warning
	case markForeign:
		return t.TextMuted
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
	case markForeign:
		return ui.Box(c).Size(size, size).Radius(size/2).Border(1.5, col).Shrink(0).
			AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Box(c).Size(size-4, 1.5).Radius(0.75).Background(col)
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
