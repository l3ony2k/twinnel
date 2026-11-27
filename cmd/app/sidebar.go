package main

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
)

// The sidebar's container: how wide it is and in which of three modes,
// as the owner's reference (a native Mac app) has it.
//
//   - Expanded: the source list as nav.go draws it, as wide as the user
//     dragged it (sideMinW…sideMaxW), the platform's width (wider in some
//     languages, sidebarExtra) until then.
//   - Icons only: a rail of the rows' icons, no section titles (a hairline
//     between sections instead), each row's name as its tooltip, a count
//     as a mark on the icon, the footer's states as their marks alone.
//   - Hidden: the page takes the whole window.
//
// The toggle right after the traffic lights, the 显示 menu's 切换侧边栏
// and ⌃⌘S cycle expanded → icons → hidden → expanded. The divider is
// dragged as an NSSplitView's: the width follows the pointer between the
// limits; under them it snaps to the rail, then to hidden, and back as
// the pointer comes out again. A double click on it does nothing, as on
// Finder's and Mail's sidebars. Changes animate with the motion tokens
// (Reduce Motion: at once). The mode and the width are kept in app.json.

// sidebarMode is the sidebar's mode.
type sidebarMode int

const (
	sideExpanded sidebarMode = iota
	sideRail
	sideHidden
)

// Widths of the sidebar in points.
const (
	// sideRailW is the rail: a row of 36 (the 18 pt icon with Finder's
	// 9 pt on either side) in the sidebar's 10 pt padding.
	sideRailW = 56
	sideMinW  = 180 // the narrowest expanded sidebar
	sideMaxW  = 340 // the widest
	// sideHandleW is the divider's grip, over both sides of the line.
	sideHandleW = 6
	// sideToggleW × sideToggleH is the title bar toggle.
	sideToggleW = 32
	sideToggleH = 28
)

// sideMotion is how the sidebar opens, closes and changes mode: its width
// in 240 ms, easing in and out, its content laid out at the end size and
// clipped on the way.
var sideMotion = ui.ElementTransition{Width: true, Duration: durMedium, Ease: easeInOut}

// navRail is true while the sidebar's rows are built as the rail
// (navList draws railRow and railGap then). Main thread only.
var navRail bool

// sidebarState is the sidebar's mode and size. Main thread only.
type sidebarState struct {
	loaded bool
	mode   sidebarMode
	// last is the mode shown before hiding it: a hidden sidebar keeps its
	// content laid out so, which slides out of and back into view.
	last  sidebarMode
	width float32 // expanded width; 0 = the default
	// While the divider is dragged: raw is where the pointer put the
	// sidebar's edge, which the mode and the width follow (sideSnap).
	dragging bool
	raw      float32
	// toggled is when the toggle last ran: the 显示 menu's key equivalent
	// and the window's own shortcut both hear ⌃⌘S, and must cycle once.
	toggled time.Time
}

// load reads the state from the app's config, once.
func (s *sidebarState) load(cfg status.Config) {
	if s.loaded {
		return
	}
	s.loaded = true
	switch cfg.Sidebar {
	case "icons":
		s.mode = sideRail
	case "hidden":
		s.mode, s.last = sideHidden, sideExpanded
	}
	if w := cfg.SidebarWidth; w > 0 {
		s.width = min(max(w, sideMinW), sideMaxW)
	}
}

// configValue is the mode as app.json keeps it.
func (m sidebarMode) configValue() string {
	switch m {
	case sideRail:
		return "icons"
	case sideHidden:
		return "hidden"
	}
	return ""
}

// expandedW is the expanded sidebar's width: the dragged one, or the
// platform's for the current language.
func (s *sidebarState) expandedW() float32 {
	if s.width > 0 {
		return s.width
	}
	return plat.sidebarWidth + sidebarExtra
}

// layoutMode is the mode the sidebar's content is laid out in.
func (s *sidebarState) layoutMode() sidebarMode {
	if s.mode == sideHidden {
		return s.last
	}
	return s.mode
}

// widthOf is the sidebar's width in a mode.
func (s *sidebarState) widthOf(m sidebarMode) float32 {
	switch m {
	case sideRail:
		return sideRailW
	case sideHidden:
		return 0
	}
	return s.expandedW()
}

// setMode changes the mode, keeping what a hidden sidebar shows.
func (s *sidebarState) setMode(m sidebarMode) {
	if m == sideHidden && s.mode != sideHidden {
		s.last = s.mode
	}
	s.mode = m
}

// next is the toggle's next mode.
func (m sidebarMode) next() sidebarMode {
	switch m {
	case sideExpanded:
		return sideRail
	case sideRail:
		return sideHidden
	}
	return sideExpanded
}

// sideSnap is the mode and the expanded width for the sidebar's edge
// dragged to raw: an expanded sidebar between the limits; under the
// narrowest, the rail once the edge is nearer to it than to the
// narrowest; hidden once the edge is nearer to the window's edge than to
// the rail. width is the expanded width to keep (unchanged when not
// expanded).
func sideSnap(raw, width float32) (sidebarMode, float32) {
	switch {
	case raw < sideRailW/2:
		return sideHidden, width
	case raw < (sideRailW+sideMinW)/2:
		return sideRail, width
	}
	return sideExpanded, min(max(raw, sideMinW), sideMaxW)
}

// sidebarFrame builds the sidebar's frame, as wide as its mode, and the
// sidebar in it (body: window.go's sidebar, laid out at sideInnerW).
func (a *app) sidebarFrame(c *ui.Context, body func()) {
	s := &a.side
	s.load(a.cfg)
	if ui.Cmd != ui.Ctrl { // macOS: ⌃⌘S, as Finder's and Mail's Show Sidebar
		c.OnWindowShortcut(ui.Ctrl|ui.Cmd, ui.KeyS, a.toggleSidebar)
	}
	frame := ui.Row(c.Key("side-frame")).Width(s.widthOf(s.mode)).Shrink(0).Clip().AlignItems(ui.Stretch)
	if !s.dragging {
		frame.Transition(sideMotion)
	}
	if s.mode == sideHidden {
		frame.AccessHidden(true)
	}
	frame.Children(func() {
		navRail = s.layoutMode() == sideRail
		defer func() { navRail = false }()
		body()
	})
}

// sideInnerW is the width the sidebar's content is laid out at.
func (a *app) sideInnerW() float32 {
	return a.side.widthOf(a.side.layoutMode())
}

// toggleSidebar cycles the sidebar's mode (the toggle, ⌃⌘S, 显示 menu).
func (a *app) toggleSidebar() {
	s := &a.side
	if time.Since(s.toggled) < 150*time.Millisecond {
		return // the same ⌃⌘S, heard by the menu and the window
	}
	s.toggled = time.Now()
	s.load(a.cfg)
	s.setMode(s.mode.next())
	a.saveSidebar()
	if a.win != nil {
		a.win.Invalidate()
	}
}

// saveSidebar keeps the mode and the width in app.json (not in shot
// mode). A failure is logged: the sidebar works on without it.
func (a *app) saveSidebar() {
	if a.shotMode {
		return
	}
	cfg := a.cfg
	cfg.Sidebar, cfg.SidebarWidth = a.side.mode.configValue(), a.side.width
	if cfg.Sidebar == a.cfg.Sidebar && cfg.SidebarWidth == a.cfg.SidebarWidth {
		return
	}
	if err := status.SaveConfig(a.cfgPath, cfg); err != nil {
		log.Printf("sidebar: %v", err)
		return
	}
	a.cfg = cfg
}

// titleBarLeft is where the window controls end: the toggle goes there.
// A hidden shot window reports no controls; it gets the usual room, so
// that captures show where the toggle will be.
func (a *app) titleBarLeft(c *ui.Context) float32 {
	bar := c.TitleBar()
	if bar.Left == 0 && a.shotMode && plat.titleBar == mygo.TitleBarHiddenInset {
		return 78
	}
	return bar.Left
}

// toolbarLead is the toolbar's left inset: the page's padding, or more
// when the sidebar no longer covers the traffic lights and the toggle.
func (a *app) toolbarLead(c *ui.Context) float32 {
	clear := a.titleBarLeft(c) + sideToggleW + 12 - a.side.widthOf(a.side.mode)
	return max(plat.pagePadX, clear)
}

// sidebarChrome builds what lies over the window's layout: the toggle on
// the title bar and the divider's grip. Built after the content, so that
// both are above it.
func (a *app) sidebarChrome(c *ui.Context, barH float32) {
	t := c.Theme()
	s := &a.side
	T := i18n.T

	// The toggle, centered on the traffic lights' line.
	lineH := c.TitleBar().Height
	if lineH == 0 {
		lineH = barH
	}
	b := ui.Button(c.Key("side-toggle"), "").Label(T.SidebarToggle).Tooltip(T.SidebarToggle).
		Absolute().Left(a.titleBarLeft(c)+4).Top((lineH-sideToggleH)/2).Size(sideToggleW, sideToggleH).
		Padding(0).Background(ui.Transparent).BorderWidth(0).Radius(plat.controlRadius).
		AlignItems(ui.Center).Justify(ui.Center)
	switch {
	case b.Pressed():
		b.Background(t.Text.Alpha(0.12))
	case b.Hovered():
		b.Background(t.Text.Alpha(0.07))
	}
	b.Children(func() {
		ui.Icon(c, iconSidebarToggle).FontSize(iconSidebar).TextColor(t.TextMuted)
	})
	if b.Clicked() {
		a.toggleSidebar()
	}

	// The divider's grip: over the line between the sidebar and the page,
	// under the title bar (which drags the window). A hidden sidebar has
	// none, unless the drag that hid it goes on.
	if s.mode == sideHidden && !s.dragging {
		return
	}
	shown := s.widthOf(s.mode)
	grip := ui.Box(c.Key("side-grip")).Absolute().Left(max(shown-sideHandleW/2, 0)).Top(barH).Bottom(0).
		Width(sideHandleW).Cursor(ui.CursorResizeEW).Role(ui.RoleSplitter).Label(T.SidebarResize)
	dx, _, held := grip.Dragged()
	switch {
	case held && !s.dragging:
		s.dragging, s.raw = true, shown+dx
	case held:
		s.raw += dx
	case s.dragging:
		s.dragging = false
		a.saveSidebar()
		return
	default:
		return
	}
	m, w := sideSnap(s.raw, s.width)
	if m == sideExpanded {
		s.width = w
	}
	if m != s.mode {
		s.setMode(m)
	}
}

// railRow is a row of the rail: the icon alone, centered, with the
// selection the platform's (navRow's colors), the page's name as its
// tooltip, and a count as a mark on the icon. It reports a click.
func railRow(c *ui.Context, it navItem, chosen, active bool) bool {
	t := c.Theme()
	rowH, _, iconSize := navMetrics()
	iconCol := t.Accent
	tip := it.label
	if it.count > 0 {
		tip = fmt.Sprintf("%s · %d", it.label, it.count)
	}
	row := ui.Row(c.Key("nav:" + it.id)).Height(rowH).AlignItems(ui.Center).Justify(ui.Center).
		Radius(navSelRadius).Tooltip(tip).Role(ui.RoleTreeItem).Label(tip).AccessSelected(chosen)
	if it.digit > 0 {
		row.KeyShortcuts(ui.Cmd, it.key)
	}
	onAccent := false
	switch plat.navSel {
	case navNeutral, navNeutralPill:
		if chosen {
			row.Background(navNeutralFill(t, active))
		}
		if !active && liquidGlass {
			iconCol = t.TextMuted
		}
	default: // navAccentFill
		row.Radius(plat.controlRadius)
		switch {
		case chosen && active:
			row.Background(t.Accent)
			iconCol, onAccent = t.AccentText, true
		case chosen:
			row.Background(navInactiveFill(t))
			iconCol = t.Text
		}
	}
	row.Children(func() {
		ui.Box(c).Size(iconSize+2, iconSize).Shrink(0).Children(func() {
			ui.Icon(c, it.icon).FontSize(iconSize).TextColor(iconCol)
			if it.count > 0 {
				railMark(c, it.tone, onAccent).Absolute().Top(-4).Right(-7)
			}
		})
	})
	return row.Clicked()
}

// railMark is a count on the rail: a plain count is a small filled dot,
// one to look at is the attention ring with a dot in it (warning color,
// or danger when something failed). Shape, not color alone, tells them
// apart.
func railMark(c *ui.Context, tone navTone, onAccent bool) ui.Element {
	t := c.Theme()
	switch {
	case tone == toneNeutral && onAccent:
		return dot(c, t.AccentText, 6).AccessHidden(true)
	case tone == toneNeutral:
		return dot(c, t.TextMuted, 6).AccessHidden(true)
	}
	col := t.Warning
	if tone == toneAlert {
		col = t.Danger
	}
	if onAccent {
		col = t.AccentText
	}
	return stateDot(c, markAttention, col, 9).AccessHidden(true)
}

// railGap stands for a section's title on the rail: a hairline, half a
// row of room.
func railGap(c *ui.Context) {
	t := c.Theme()
	rowH, _, _ := navMetrics()
	ui.Column(c).Height(rowH / 2).Justify(ui.Center).PaddingX(8).AccessHidden(true).Children(func() {
		ui.Box(c).Height(1).Background(pal(t).separator)
	})
}

// sideFooter is the sidebar's foot: the duet pair, or on the rail its
// marks alone, one under another, each with its words as tooltip.
func (a *app) sideFooter(c *ui.Context, s status.Snapshot) {
	if !navRail {
		a.duetPair(c, s, 6)
		return
	}
	a.railPair(c, s)
}

// railPair is duetPair's states on the rail. It follows duetPair's
// shapes and colors (sidebarstate.go); keep the two alike.
func (a *app) railPair(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	a.windowKeys(c)
	mark := func(key, label string, m stateMark, col ui.Color) {
		ui.Row(c.Key(key)).Size(24, 18).AlignItems(ui.Center).Justify(ui.Center).
			Tooltip(label).Label(label).Role(ui.RoleStatus).Children(func() {
			stateDot(c, m, col, 8)
		})
	}
	ui.Column(c).Gap(4).PaddingY(8).AlignItems(ui.Center).Children(func() {
		mstate, mtext := markDown, T.Unreachable
		if s.Mihomo.Reachable {
			mstate, mtext = markOK, T.Running
		}
		mark("m", i18n.F(T.StateLabel, T.Mihomo, mtext), mstate, stateMarkColor(t, mstate))
		ttext := T.Unreachable
		if s.Tailscale.Reachable {
			ttext = T.TSState(s.Tailscale.State.BackendState)
		}
		mark("t", i18n.F(T.StateLabel, T.Tailscale, ttext), tsMark(s), tsColor(t, s))
		hm, hcol := markDown, t.TextMuted
		switch s.Helper.State {
		case status.HelperConnected:
			hm, hcol = markOK, t.Success
		case status.HelperNotRunning, status.HelperDenied, status.HelperNotInstalled:
			hm, hcol = markAttention, t.Warning
		}
		if helperOutdated(s) {
			mark("h", T.HelperNeedsUpdate, markAttention, t.Warning)
		} else {
			mark("h", i18n.F(T.StateLabel, T.Helper, s.Helper.State.Text()), hm, hcol)
		}
		if !s.WritesAllowed {
			ui.Row(c.Key("ro")).Size(24, 18).AlignItems(ui.Center).Justify(ui.Center).
				Tooltip(T.ReadOnlyShort + T.LabelSep + T.WritesDisabledHint).
				Label(T.ReadOnlyShort + T.LabelSep + T.WritesDisabledHint).Role(ui.RoleStatus).Children(func() {
				ui.Icon(c, iconSecure).FontSize(iconSmall).TextColor(t.TextMuted)
			})
		}
	})
}
