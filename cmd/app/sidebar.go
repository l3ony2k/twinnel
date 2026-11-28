package main

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// The sidebar's container: how wide it is and in which of three modes,
// as the owner's reference (a native Mac app) has it.
//
//   - Expanded: the source list as nav.go draws it, as wide as the user
//     dragged it (between the measured labels' width and sideMaxW), the
//     platform's width until then, or the labels' when they need more.
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

// sideRailW is the rail's width: as wide as the traffic lights' cluster
// with its margin on both sides (19 + 60 + 19 = 98 pt on macOS 26), so the
// rail's material runs under the lights as every Mac sidebar's does, and
// the lights never hang over the page. Without lights (full screen, other
// platforms) a row of 36 in the sidebar's 10 pt padding. Set per frame
// (sidebarFrame); main thread only.
var sideRailW float32 = sideRailMin

// Widths of the sidebar in points.
const (
	sideRailMin = 56
	sideMinW    = 180 // the narrowest expanded sidebar, unless its labels need more
	sideMaxW    = 340 // the widest
	// railRowW is a rail row's width (its selection), centered in the rail.
	railRowW = 52
	// sideHandleW is the divider's grip, over both sides of the line.
	sideHandleW = 6
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
	// need is the narrowest expanded width that shows every label whole,
	// measured in the current language (sidebarNeed); needKey is what it
	// was measured for.
	need    float32
	needKey string
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
		s.width = min(max(w, sideMinW), sideMaxW) // and at least need, expandedW
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

// minW is the narrowest expanded width: sideMinW, or what the labels need.
func (s *sidebarState) minW() float32 {
	return min(max(sideMinW, s.need), sideMaxW)
}

// expandedW is the expanded sidebar's width: the dragged one, or the
// platform's; never narrower than its labels need.
func (s *sidebarState) expandedW() float32 {
	if s.width > 0 {
		return max(s.width, s.minW())
	}
	return max(plat.sidebarWidth, s.minW())
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
func sideSnap(raw, width, minW float32) (sidebarMode, float32) {
	switch {
	case raw < sideRailW/2:
		return sideHidden, width
	case raw < (sideRailW+minW)/2:
		return sideRail, width
	}
	return sideExpanded, min(max(raw, minW), sideMaxW)
}

// sidebarFrame builds the sidebar's frame, as wide as its mode, and the
// sidebar in it (body: window.go's sidebar, laid out at sideInnerW).
func (a *app) sidebarFrame(c *ui.Context, body func()) {
	s := &a.side
	s.load(a.cfg)
	sideRailW = railWidth(c)
	if key := i18n.T.PageOverview + i18n.T.Helper; key != s.needKey {
		s.need, s.needKey = sidebarNeed(c), key
	}
	if ui.Cmd != ui.Ctrl { // macOS: ⌃⌘S, as Finder's and Mail's Show Sidebar
		c.OnWindowShortcut(ui.Ctrl|ui.Cmd, ui.KeyS, a.toggleSidebar)
	}
	if plat.sidebarKey != 0 { // Linux: F9, as GNOME's apps (Files, Settings)
		c.OnWindowShortcut(0, plat.sidebarKey, a.toggleSidebar)
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

// The traffic lights on macOS 26, measured on the owner's screen (a real
// window, 2x): three 14 pt circles 23 pt apart, the green one ending at
// TitleBar().Left (79), their bottom at TitleBar().Bottom (33). Their
// center line is 26 pt down: a unified toolbar's 52 pt band is centered
// on it. TitleBar().Height (66 under the hidden-inset style) is not.
const (
	trafficLightD     = 14 // a light's diameter
	trafficLightPitch = 23 // from one light's left edge to the next's
	// sideToggleGap is the room between the green light and the toggle's
	// capsule, as Finder's and Xcode's sidebar item on macOS 26.
	sideToggleGap = 14
)

// trafficLightsMid is the traffic lights' center line from the window's
// top: the line the toolbar's items are centered on. Without lights it
// is the middle of the title bar the window draws (plat.titleBarHeight:
// Linux centers its window buttons in 52, Windows 11's caption buttons
// fill 48), or of a 52 pt band where there is none (full screen, a
// compositor that decorates the window).
func trafficLightsMid(c *ui.Context) float32 {
	if !plat.trafficLights {
		if h := c.TitleBar().Height; h > 0 {
			return h / 2
		}
		return 26
	}
	if bar := c.TitleBar(); bar.Bottom > 0 {
		return bar.Bottom - trafficLightD/2
	}
	return 26
}

// sidebarTop is where the sidebar's rows start: under the toolbar band,
// which the toggle's capsule (and the traffic lights) own.
func sidebarTop(c *ui.Context) float32 {
	return toolbarHeight(c) + 4
}

// railWidth is the rail's width for this window (sideRailW).
func railWidth(c *ui.Context) float32 {
	bar := c.TitleBar()
	if bar.Left <= 0 || !plat.trafficLights {
		return sideRailMin // window buttons on the left (Linux) are the toggle's business: sideToggleX
	}
	first := bar.Left - (2*trafficLightPitch + trafficLightD) // the red light's left edge
	return max(sideRailMin, bar.Left+first)
}

// sideToggleX is the toggle's left edge. Over the expanded sidebar or a
// hidden one: after the lights and a gap, as Finder's and Xcode's. Beside
// the rail, which the lights fill: at the start of the page's toolbar.
// Without lights: the window's inset.
func (a *app) sideToggleX(c *ui.Context) float32 {
	l := c.TitleBar().Left
	if a.side.mode == sideRail {
		x := sideRailW + sideToggleRailInset
		if !plat.trafficLights && l > 0 { // the desktop's buttons on the left may be wider than the rail
			x = max(x, l+sideToggleRailInset)
		}
		return x
	}
	if l > 0 {
		if !plat.trafficLights {
			return l + sideToggleRailInset
		}
		return l + sideToggleGap
	}
	return 12
}

// sideToggleRailInset is the toggle's inset from the rail's edge: a
// toolbar item's place at the start of the page's toolbar.
const sideToggleRailInset = 8

// sideToggleW is the toggle's hover shape: a toolbar capsule's insets
// around the sidebar icon.
const sideToggleW = 2*toolbarPadX + iconSidebar

// toolbarLead is the toolbar's left inset: the page's padding, or more
// when the sidebar no longer covers the traffic lights and the toggle.
func (a *app) toolbarLead(c *ui.Context) float32 {
	clear := a.sideToggleX(c) + sideToggleW + toolbarGap - a.side.widthOf(a.side.mode)
	return max(plat.pagePadX, clear)
}

// contentHairline is whether the page draws the line at its left edge:
// beside the sidebar or the rail, not when the sidebar is hidden.
func (a *app) contentHairline() bool {
	return a.side.mode != sideHidden
}

// sidebarNeed measures the narrowest expanded sidebar that shows every
// row's label whole with its count (two digits), every section title,
// and the foot's lines, in the current language and type sizes. The
// metrics are navRow's and twinnelPair's.
func sidebarNeed(c *ui.Context) float32 {
	_, _, iconSize := navMetrics()
	text := func(s string, size float32, weight int) float32 {
		w, _ := c.MeasureText(0, ui.Span{Text: s, Size: size, Weight: weight})
		return float32(math.Ceil(float64(w)))
	}
	const sidePad, rowPad, gap = 10, 9, 8
	badge := max(navBadgeH, text("88", plat.smallSize, 600)+12)
	need := float32(0)
	for _, sec := range navSections(88, 88, toneAttention) {
		need = max(need, 2*sidePad+7+text(sec.title, plat.smallSize, 600))
		for _, it := range sec.items {
			w := 2*sidePad + 2*rowPad + iconSize + 2 + gap + text(it.label, plat.bodySize, 0)
			if it.count > 0 {
				w += gap + badge
			}
			need = max(need, w)
		}
	}
	// The foot: one line per state, the mark in an icon-wide box, the
	// nav gap, the name; the helper's action line ends in a chevron.
	T := i18n.T
	foot := []string{T.Mihomo, T.Tailscale, T.HelperShort, T.HelperNeedsUpdate, T.HelperDownRow,
		T.Helper + " · " + T.HelperNotInstalled}
	for _, l := range foot {
		need = max(need, 2*sidePad+2*navRowPadX()+iconSize+2+navGap+text(l, plat.smallSize, 0)+4+iconSmall)
	}
	return need
}

// sidebarChrome builds what lies over the window's layout: the toggle on
// the title bar and the divider's grip. Built after the content, so that
// both are above it.
func (a *app) sidebarChrome(c *ui.Context, barH float32) {
	s := &a.side
	T := i18n.T

	if shotLights {
		lightsOverlay(c)
	}
	// The toggle: a title bar button as Finder's and Xcode's on macOS 26,
	// a bare glyph at rest, a toolbar capsule's fill on hover and press
	// (no glass, no face), centered on the traffic lights' line.
	t := c.Theme()
	b := ui.Button(c.Key("side-toggle"), "").Label(T.SidebarToggle).Tooltip(T.SidebarToggle).
		Absolute().Left(a.sideToggleX(c)).Top(trafficLightsMid(c)-toolbarItemH/2).
		Size(sideToggleW, toolbarItemH).Padding(0).Radius(toolbarItemH / 2).
		AlignItems(ui.Center).Justify(ui.Center).BorderWidth(0).Background(ui.Transparent)
	switch {
	case b.Pressed() || toolbarForce == "pressed":
		b.Background(t.Text.Alpha(0.12))
	case b.Hovered() || toolbarForce == "hover":
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
	m, w := sideSnap(s.raw, s.width, s.minW())
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
	row := ui.Row(c.Key("nav:" + it.id)).Width(railRowW).AlignSelf(ui.Center).Height(rowH).AlignItems(ui.Center).Justify(ui.Center).
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
			if it.count > 0 && it.tone != toneNeutral {
				railMark(c, it.tone, onAccent).Absolute().Top(-4).Right(-7)
			}
		})
	})
	return row.Clicked()
}

// railMark is a count to look at on the rail: the attention ring with a
// dot in it (warning color, or danger when something failed). A neutral
// count has no mark there (a gray dot said nothing); the row's tooltip
// still gives it.
func railMark(c *ui.Context, tone navTone, onAccent bool) ui.Element {
	t := c.Theme()
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
	ui.Column(c).Width(railRowW).AlignSelf(ui.Center).Height(rowH / 2).Justify(ui.Center).PaddingX(8).AccessHidden(true).Children(func() {
		ui.Box(c).Height(1).Background(pal(t).separator)
	})
}

// sideFooter is the sidebar's foot: the Twinnel pair, or on the rail its
// marks alone, one under another, each with its words as tooltip.
func (a *app) sideFooter(c *ui.Context, s status.Snapshot) {
	if !navRail {
		a.twinnelPair(c, s, navRowPadX()) // on the nav rows' icon column
		return
	}
	a.railPair(c, s)
}

// railPair is twinnelPair's states on the rail. It follows twinnelPair's
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
		mstate, mcol, mtext := mihomoMark(t, s.MihomoView())
		mark("m", i18n.F(T.StateLabel, T.Mihomo, mtext), mstate, mcol)
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
	})
}
