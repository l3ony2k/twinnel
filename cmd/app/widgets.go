package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/msg"
	"github.com/l3ony2k/twinnel/core/status"
)

// The app's design tokens and shared pieces. Every page builds from these
// so sizes, colors and type stay one system (diagnosis: "design system
// pass"). Platform values come from plat (platform.go).
//
// Text roles: primary (t.Text), secondary (t.TextMuted), and status text
// (pal.okText / warnText / badText), which is darker than the dot colors in
// the light appearance so it stays readable.

// palette is what the theme does not name.
type palette struct {
	group      ui.Color // fill of a grouped box
	groupLine  ui.Color // its hairline
	separator  ui.Color // lines inside a group
	okText     ui.Color
	warnText   ui.Color
	badText    ui.Color
	accentSoft ui.Color // selected chip / row fill
	code       ui.Color // code and log backgrounds
}

func pal(t *ui.Theme) palette {
	if t.Dark {
		return palette{
			group:      ui.RGBA(255, 255, 255, 0.045),
			groupLine:  ui.RGBA(255, 255, 255, 0.07),
			separator:  ui.RGBA(255, 255, 255, 0.08),
			okText:     t.Success,
			warnText:   t.Warning,
			badText:    t.Danger,
			accentSoft: t.Accent.Alpha(0.22),
			code:       ui.RGBA(0, 0, 0, 0.25),
		}
	}
	return palette{
		group:      ui.Hex("#ffffff"),
		groupLine:  ui.RGBA(0, 0, 0, 0.07),
		separator:  ui.RGBA(0, 0, 0, 0.08),
		okText:     ui.Hex("#248a3d"),
		warnText:   ui.Hex("#c93400"),
		badText:    ui.Hex("#d70015"),
		accentSoft: t.Accent.Alpha(0.12),
		code:       ui.RGBA(0, 0, 0, 0.035),
	}
}

// Spacing steps (4 px grid).
const (
	gapRow    = 8  // between rows inside a group (System Settings' rhythm)
	gapGroup  = 20 // between groups on a page
	groupPadX = 16 // a group's side padding; its heading lines up with it
	groupPadY = 12
	gapSub    = 2 // between a line and the secondary line under it
)

// labelWidth is the label column of kv rows as the current language sets
// it (langLayouts). Rows read labelCol, which a wider label widens.
var labelWidth float32 = 96

// labelGrown is the widest kv label measured since the language was set,
// when wider than labelWidth: a system font wider than the one the
// default was set for (Windows' Microsoft YaHei UI) widens the column
// instead of truncating "Twinnel 偏好设置". Every row shares it, so the
// column stays aligned; setLayout resets it.
var labelGrown float32

// labelMax is the widest the label column grows: a longer label wraps
// onto a second line (labelWraps) rather than squeezing the values or
// being cut ("后台助手服务（Windows 服务）" in Microsoft YaHei UI).
const labelMax = 200

// labelCol is the label column kv rows share.
func labelCol() float32 { return max(labelWidth, labelGrown) }

// growLabel widens the column to need (capped at labelMax) and reports
// whether it grew.
func growLabel(need float32) bool {
	need = min(need, labelMax)
	if need <= labelCol() {
		return false
	}
	labelGrown = need
	return true
}

// labelWraps reports whether a label that needs need wraps in a column
// extra wider than labelCol: only one wider than labelMax does, as the
// column grows to fit any other.
func labelWraps(need, extra float32) bool { return need > labelCol()+extra }

// kvLabel is a kv row's label column: the label, with an ⓘ after it when
// tip is not empty, extra wider than labelCol. The column widens to fit
// the label (growLabel) and redraws once so earlier rows line up.
func kvLabel(c *ui.Context, label, tip string, more func(), extra float32) ui.Element {
	t := c.Theme()
	need := textWidth(c, ui.Span{}, label)
	if tip != "" {
		need += 4 + infoTipSize
	}
	if growLabel(need - extra) {
		c.Invalidate()
	}
	// A label wider than the capped column wraps (at the text line
	// height) instead of being cut; every other label is one line.
	text := func(e ui.Element) ui.Element {
		if labelWraps(need, extra) {
			return e.Wrap().LineHeight(lineText)
		}
		return e.SingleLine()
	}
	if tip == "" {
		return text(ui.Text(c, label).TextColor(t.TextMuted).Width(labelCol() + extra).Shrink(0))
	}
	return ui.Row(c).Gap(4).AlignItems(ui.Center).Width(labelCol() + extra).Shrink(0).Children(func() {
		text(ui.Text(c, label).TextColor(t.TextMuted).Shrink(1).MinWidth(0))
		infoTipAbout(c, label, tip, more)
	})
}

// lineText is the line height of text that wraps, as a multiple of its
// size: every paragraph (hints, notes, errors, row details) spaces its
// lines alike, CJK and Latin mixed, whatever font the fallback picks.
const lineText = 1.4

// Toolbar items (Liquid Glass, macOS 26+): every button and segmented
// control is a capsule of glass this tall, its hover and pressed fill the
// capsule itself (one shape for the glass rim, the fill and the focus
// ring); labels and switches sit bare on the bar, centered on the same
// line, as in Finder's and System Settings' toolbars.
const (
	toolbarItemH  = 32 // a capsule's height; its radius is half of it
	toolbarPadX   = 12 // a capsule's side inset to its label or icon
	toolbarGap    = 8  // between items, and from a label to its control
	toolbarSegPad = 4  // a segmented capsule's inset to its segments
)

// Icon sizes: one per role.
const (
	iconNav     = 16 // sidebar before macOS 26, row status marks
	iconSidebar = 18 // sidebar on macOS 26+ (Finder's medium symbols, 17–19 pt)
	iconInline  = 14 // in buttons, headings, notices, rows
	iconSmall   = 12 // disclosure chevrons, tiny keys
	iconLarge   = 22 // the verdict and empty states
	iconHero    = 28 // the app mark on the welcome page
)

// Motion tokens (vibedesign 011). One curve per role; no call site picks
// its own. Transitions stop by themselves under Reduce Motion (MyGo).
var (
	easeOut       = ui.CubicBezier(0, 0, .58, 1)   // things entering, hover
	easeOutStrong = ui.CubicBezier(.22, 1, .36, 1) // small parts settling after a click
	easeIn        = ui.CubicBezier(.42, 0, 1, 1)   // things leaving
	easeInOut     = ui.CubicBezier(.42, 0, .58, 1) // size or place changes of something on screen
)

const (
	durHover  = 100 * time.Millisecond // hover and selection fills: about instant, as Mac lists
	durSmall  = 180 * time.Millisecond // closing, small parts
	durMedium = 240 * time.Millisecond // opening a row
)

// Transitions built from the tokens.
var (
	// expandOpen and expandClose: a row that opens grows to its new
	// height in 240 ms, one that closes shrinks in 180, both easing in
	// and out (the row stays on screen). Use expandFor(open).
	expandOpen  = ui.ElementTransition{Duration: durMedium, Ease: easeInOut}
	expandClose = ui.ElementTransition{Duration: durSmall, Ease: easeInOut}
	// expandMotion is expandOpen, for callers that do not know the state.
	expandMotion = expandOpen
	// colorMotion animates only colors (hover and selection fills), never
	// position or size: tiles must not slide behind a window resize.
	colorMotion = ui.ElementTransition{Colors: true, Duration: durHover, Ease: easeOut}
	// fadeMotion fades an element in and out (the toolbar's scroll edge).
	fadeMotion = ui.ElementTransition{
		Duration: 150 * time.Millisecond, Ease: easeOut,
		ExitDuration: 120 * time.Millisecond, ExitEase: easeIn,
		Enter: &ui.Motion{Opacity: 0}, Exit: &ui.Motion{Opacity: 0},
	}
	// turnMotion turns a disclosure chevron.
	turnMotion = ui.ElementTransition{Duration: 200 * time.Millisecond, Ease: easeOutStrong}
	// statusMotion is a status row whose state changes in place (the
	// apply card's 已应用 / 应用失败, a service's state): its colors
	// cross-fade over 180 ms, and a row that replaces another fades in.
	// Key the row by its state so a new state is a new row.
	statusMotion = ui.ElementTransition{
		Colors: true, Duration: durSmall, Ease: easeOut,
		ExitDuration: -1, // the old state goes at once; the new one fades in over it
		Enter:        &ui.Motion{Opacity: 0},
	}
	// popoverMotion is how a popover or menu panel comes from its trigger
	// (kubai R-15): 150 ms in from 4 pt toward the trigger at 96 %, out
	// in 100; with AttachedOrigin it grows from the edge nearest the
	// trigger and flips with the panel.
	popoverMotion = ui.ElementTransition{
		Duration: 150 * time.Millisecond, Ease: easeOut,
		ExitDuration: 100 * time.Millisecond, ExitEase: easeIn,
		Enter: &ui.Motion{Opacity: 0, Scale: 0.96, Y: -4, Flip: true},
		Exit:  &ui.Motion{Opacity: 0, Scale: 0.96, Y: -4, Flip: true},
	}
)

// expandFor is the transition of a row whose open state is open in this
// frame: the frame that flips it builds the row with the new value.
func expandFor(open bool) ui.ElementTransition {
	if open {
		return expandOpen
	}
	return expandClose
}

// disclosure draws the chevron of a row that opens in place: one
// chevron-right turned 90° while open, as NSOutlineView's triangle.
// linkDisclosure is a small accent link with a turning chevron that shows
// or hides more text under it (a file's content, a full report). It is
// focusable and spoken as a disclosure; the caller checks Clicked.
func linkDisclosure(c *ui.Context, open bool, label string) ui.Element {
	t := c.Theme()
	var turn float32
	if open {
		turn = 90
	}
	d := ui.Row(c).Gap(4).AlignItems(ui.Center).
		Focusable().Role(ui.RoleDisclosure).Expanded(open).Label(label)
	d.Children(func() {
		ui.Icon(c, iconCollapsed).FontSize(iconSmall).TextColor(t.Accent).Rotate(turn).Transition(turnMotion)
		ui.Text(c, label).FontSize(plat.smallSize).TextColor(t.Accent)
	})
	return d
}

func disclosure(c *ui.Context, open bool) ui.Element {
	var turn float32
	if open {
		turn = 90
	}
	return ui.Icon(c, iconCollapsed).FontSize(iconSmall).TextColor(c.Theme().TextMuted).
		Width(14).Rotate(turn).Transition(turnMotion)
}

// levelColor is the dot color of a status level.
func levelColor(t *ui.Theme, l status.Level) ui.Color {
	switch l {
	case status.LevelOK:
		return t.Success
	case status.LevelWarn:
		return t.Warning
	case status.LevelError:
		return t.Danger
	}
	return t.TextMuted
}

// levelText is the text color of a status level.
func levelText(t *ui.Theme, l status.Level) ui.Color {
	p := pal(t)
	switch l {
	case status.LevelWarn:
		return p.warnText
	case status.LevelError:
		return p.badText
	}
	return t.Text
}

// dot draws a status dot.
func dot(c *ui.Context, col ui.Color, size float32) ui.Element {
	return ui.Box(c).Size(size, size).Radius(size / 2).Background(col).Shrink(0)
}

// num styles a figure that changes in place: digits of one width.
func num(e ui.Element) ui.Element { return e.FontFeatures("tnum") }

// mono sets e in the system's monospaced face (SF Mono on macOS), with
// tabular digits, one step under body (SF Mono reads larger than SF Pro
// at one size). It is for technical values people compare or copy:
// PIDs, paths, service labels, IPs and ports, versions, hashes, byte
// sizes and rates in tables, rule strings, hostnames, process names.
// Never for prose or names.
func mono(e ui.Element) ui.Element {
	return e.Font("monospace").FontFeatures("tnum").FontSize(plat.calloutSize)
}

// monoText is a technical value as text (mono).
func monoText(c *ui.Context, s string) ui.Element { return mono(ui.Text(c, s)) }

// small draws secondary text.
func small(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(plat.calloutSize).TextColor(c.Theme().TextMuted).LineHeight(lineText)
}

// kv draws a label and a value on one row.
func kv(c *ui.Context, label, value string) {
	kvWith(c, label, func() {
		num(ui.Text(c, value).Grow(1).SingleLine().Selectable())
	})
}

// kvMono is kv for a technical value (mono): a path, a PID, an address.
func kvMono(c *ui.Context, label, value string) {
	kvWith(c, label, func() {
		monoText(c, value).Grow(1).SingleLine().Selectable().Tooltip(value)
	})
}

// kvWith draws a label and custom content on one row.
func kvWith(c *ui.Context, label string, body func()) {
	kvTipWith(c, label, "", nil, body)
}

// kvTip is kv with an ⓘ after the label (infoTip): what the row means.
func kvTip(c *ui.Context, label, tip, value string) {
	kvTipWith(c, label, tip, nil, func() {
		num(ui.Text(c, value).Grow(1).SingleLine().Selectable())
	})
}

// kvTipWith is kvWith with an ⓘ after the label when tip is not empty;
// more is its 了解更多.
func kvTipWith(c *ui.Context, label, tip string, more func(), body func()) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Children(func() {
		kvLabel(c, label, tip, more, 0)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Children(body)
	})
}

// kvControl fits a control (a select, a segmented control, a field) into
// a kv row: the row's height, so a row holding one lines up with its
// plain neighbours, at its natural width, at the start of the values
// column (never stretched across it), and no wider than 260.
func kvControl(e ui.Element) ui.Element {
	// No Grow or Shrink of its own: setting either, even to 0, makes
	// MyGo size a segmented control from a zero basis, narrower than its
	// segments.
	return e.Height(plat.rowMinHeight).MaxWidth(260)
}

// sectionTitle draws a heading with a status dot and a state on the right.
func sectionTitle(c *ui.Context, title string, m stateMark, col ui.Color, right string) {
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		stateDot(c, m, col, 8)
		ui.Text(c, title).FontWeight(plat.headingWeight)
		ui.Spacer(c)
		if right != "" {
			num(ui.Text(c, right).TextColor(t.TextMuted).SingleLine())
		}
	})
}

// The alignment grid of a page: the page title (toolbar), every group
// heading and every group box share one left edge, the column's; a
// heading's trailing actions end on the box's right edge. Text inside a
// box sits groupPadX in from it. Headings are built by groupHead only.

// groupHead is the row above a group box: flush with the box's edges
// (no inset of its own), 28 tall so a heading with or without a button
// keeps its place.
func groupHead(c *ui.Context) ui.Element {
	return ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(28)
}

// card is a group of the main window, as System Settings draws them: the
// heading above a quiet box. An empty title draws only the box.
func card(c *ui.Context, title string, body func()) ui.Element {
	return cardWith(c, title, nil, body)
}

// cardWith is card with something at the right of the heading (a button,
// a count).
func cardWith(c *ui.Context, title string, trailing func(), body func()) ui.Element {
	return cardTipWith(c, title, "", nil, trailing, body)
}

// cardTip is card with an ⓘ after the title (infoTip): what the group is
// for; more is its 了解更多.
func cardTip(c *ui.Context, title, tip string, more func(), body func()) ui.Element {
	return cardTipWith(c, title, tip, more, nil, body)
}

// cardTipWith is cardWith with an ⓘ after the title when tip is not empty.
func cardTipWith(c *ui.Context, title, tip string, more func(), trailing func(), body func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(8).Children(func() {
		if title != "" || trailing != nil {
			groupHead(c).Children(func() {
				ui.Text(c, title).FontSize(plat.headingSize).FontWeight(plat.headingWeight).SingleLine()
				if tip != "" {
					infoTipAbout(c, title, tip, more)
				}
				ui.Spacer(c)
				if trailing != nil {
					trailing()
				}
			})
		}
		groupBox(c, t).Children(body)
	})
}

// headedCard is card with a heading row the caller builds (title, hints,
// buttons), above the box.
func headedCard(c *ui.Context, head func(), body func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(8).Children(func() {
		groupHead(c).Children(head)
		groupBox(c, t).Children(body)
	})
}

// groupBox is the box of a group, without heading.
func groupBox(c *ui.Context, t *ui.Theme) ui.Element {
	p := pal(t)
	return ui.Column(c).Padding(groupPadY, groupPadX).Gap(gapRow).Radius(plat.groupRadius).
		Background(p.group).Border(1, p.groupLine)
}

// listBox is a group box whose children are rows split by hairlines.
// The hairlines run edge to edge; the box has no padding above its first
// row or under its last, and clips to its radius, so a row's fill
// (rowFill) covers the row from hairline to hairline and from edge to
// edge, rounded with the box on the first and last row. Text keeps the
// box's side inset (groupPadX).
func listBox(c *ui.Context, t *ui.Theme) ui.Element {
	return groupBox(c, t).Padding(0, groupPadX).Gap(1).Dividers(1, pal(t).separator).Clip()
}

// listCard is a group whose rows are split by hairlines (listBox), with
// a heading row above it built by head (icon, title, actions); nil head
// draws only the list. Each row is a listRow.
func listCard(c *ui.Context, head func(), body func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(8).Children(func() {
		if head != nil {
			groupHead(c).Children(head)
		}
		listBox(c, t).Children(body)
	})
}

// listHead is the usual heading of a listCard: an optional line icon,
// the title, then trailing (buttons) at the right.
func listHead(c *ui.Context, icon *ui.SVG, title string, trailing func()) func() {
	return listHeadTip(c, icon, title, "", trailing)
}

// listHeadTip is listHead with an ⓘ after the title when tip is not
// empty.
func listHeadTip(c *ui.Context, icon *ui.SVG, title, tip string, trailing func()) func() {
	return func() {
		t := c.Theme()
		if icon != nil {
			ui.Icon(c, icon).FontSize(iconInline).TextColor(t.TextMuted)
		}
		ui.Text(c, title).FontSize(plat.headingSize).FontWeight(plat.headingWeight).SingleLine()
		if tip != "" {
			infoTipAbout(c, title, tip, nil)
		}
		ui.Spacer(c)
		if trailing != nil {
			trailing()
		}
	}
}

// listRowPadY is the space above and under a list row's content.
const listRowPadY = 10

// listRow is one row of a listCard: its lines stacked with the gap of a
// group's rows.
func listRow(c *ui.Context) ui.Element {
	return ui.Column(c).Gap(gapRow - 4).PaddingY(listRowPadY)
}

// rowBleed makes e, a row (or a row's head) in a listBox, reach the box's
// edges: pulled out over the box's side padding and padded back, so its
// fill spans the row while its content stays on the box's inset.
func rowBleed(e ui.Element) ui.Element {
	return e.MarginX(-groupPadX).PaddingX(groupPadX)
}

// rowFill is the one fill of a clickable row in a listBox (an override, a
// layer, a plan, an entry that opens a page): a faint fill while hovered
// or pressed, the accent tint while selected. e must reach the box's
// edges (rowBleed, or a column that does), and has no radius of its own:
// the box's clip rounds the first and last row.
func rowFill(c *ui.Context, e ui.Element, selected bool) ui.Element {
	t := c.Theme()
	switch {
	case selected:
		e.Background(pal(t).accentSoft)
	case e.Pressed():
		e.Background(t.Text.Alpha(0.07))
	case e.Hovered() || rowForce == "hover":
		e.Background(t.Text.Alpha(0.04))
	}
	return e
}

// hintText is a line of secondary, wrapping text: what a group is for.
func hintText(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(plat.calloutSize).TextColor(c.Theme().TextMuted).Wrap().LineHeight(lineText)
}

// pageHeader puts the page's title, and its actions at the right, in
// the window's toolbar (mainView builds it after the page).
func pageHeader(c *ui.Context, title string, trailing func()) {
	pageBar = toolbarSpec{title: title, trailing: trailing}
}

// meta draws a quiet label: a type, a role, a count. Badges are for
// things that call for attention, not for metadata.
func meta(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(plat.smallSize).TextColor(c.Theme().TextMuted).SingleLine().Shrink(0)
}

// tag draws a small outlined label for a state worth noticing (使用中,
// 托管): quieter than a filled badge.
func tag(c *ui.Context, s string) ui.Element {
	t := c.Theme()
	return ui.Row(c).Padding(1, 6).Radius(plat.controlRadius-2).Border(1, t.Border).Shrink(0).Children(func() {
		ui.Text(c, s).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine()
	})
}

// notice is one line of information with an icon, for page-level states
// such as read-only mode. Errors use errorNote.
func notice(c *ui.Context, icon *ui.SVG, text string) ui.Element {
	t := c.Theme()
	return ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(8, 12).Radius(plat.groupRadius).
		Background(pal(t).group).Border(1, pal(t).groupLine).Children(func() {
		ui.Icon(c, icon).FontSize(iconInline).TextColor(t.TextMuted)
		ui.Text(c, text).FontSize(plat.calloutSize).TextColor(t.TextMuted).Grow(1).Wrap().LineHeight(lineText)
	})
}

// errorNote shows what failed, next to where it failed. A network error
// Twinnel knows (i18n.PlainNetError: 没能连上 GitHub …) is said in words, the
// raw error under it small and selectable, as unreachableState does.
func errorNote(c *ui.Context, text string) ui.Element {
	t := c.Theme()
	raw := strings.TrimPrefix(text, i18n.T.ErrorPrefix)
	plain := i18n.PlainNetError(raw)
	return ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, iconError).FontSize(iconInline).TextColor(t.Danger).MarginY(1)
		if plain == "" {
			ui.Text(c, text).FontSize(plat.calloutSize).TextColor(pal(t).badText).Grow(1).Wrap().LineHeight(lineText).Selectable()
			return
		}
		ui.Column(c).Gap(gapSub).Grow(1).Shrink(1).Children(func() {
			ui.Text(c, plain).FontSize(plat.calloutSize).TextColor(pal(t).badText).Wrap().LineHeight(lineText).Selectable()
			ui.Text(c, raw).Font("monospace").FontSize(plat.smallSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText).
				Selectable().Label(i18n.T.UnreachableDetail + "：" + raw)
		})
	})
}

// warningNote shows a warning from the helper (compose, apply, takeover),
// in the UI language when Twinnel knows it (i18n.Warning).
// One Twinnel has no words for shows a generic line with the text behind
// 复制原文 (msgNote, U57).
func warningNote(c *ui.Context, w string) ui.Element {
	return msgNote(c, msg.Parse(w), true)
}

// infoNote is a line worth knowing that asks nothing of the user: the
// muted info glyph and text (not the warning colors).
func infoNote(c *ui.Context, text string) ui.Element {
	t := c.Theme()
	return ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, iconAbout).FontSize(iconInline).TextColor(t.TextMuted).MarginY(1)
		ui.Text(c, text).FontSize(plat.calloutSize).TextColor(t.TextMuted).Grow(1).Wrap().LineHeight(lineText).Selectable()
	})
}

// emptyState says why a list is empty and what to do about it.
func emptyState(c *ui.Context, icon *ui.SVG, title, hint string) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(6).AlignItems(ui.Center).Padding(28, 16).Children(func() {
		ui.Icon(c, icon).FontSize(iconLarge).TextColor(t.TextMuted)
		ui.Text(c, title).FontWeight(500)
		if hint != "" {
			ui.Text(c, hint).FontSize(plat.calloutSize).TextColor(t.TextMuted).TextAlign(ui.Center).Wrap().LineHeight(lineText).MaxWidth(420)
		}
	})
}

// cardEmpty is the empty state at a card's size (Vibe Design Tips 022,
// size b): one row of a 40 × 40 tile holding the place's icon at 22, and
// the title alone, in the muted text; the card's heading stays above it.
func cardEmpty(c *ui.Context, icon *ui.SVG, title string) ui.Element {
	t := c.Theme()
	return ui.Row(c).Gap(12).AlignItems(ui.Center).Role(ui.RoleStatus).Label(title).Children(func() {
		ui.Box(c).Size(40, 40).Radius(plat.innerRadius).Background(t.TextMuted.Alpha(0.12)).Shrink(0).
			AlignItems(ui.Center).Justify(ui.Center).AccessHidden(true).Children(func() {
			ui.Icon(c, icon).FontSize(iconLarge).TextColor(t.TextMuted)
		})
		ui.Text(c, title).TextColor(t.TextMuted).Grow(1).SingleLine()
	})
}

// delayColor colors a delay: plain while fast, orange when slow, red when
// it failed or is very slow. Green would be on every row and mean nothing.
func delayColor(t *ui.Theme, ms int, tested bool) ui.Color {
	p := pal(t)
	switch {
	case !tested:
		return t.TextMuted
	case ms <= 0:
		return p.badText
	case ms < 300:
		return t.TextMuted
	case ms < 800:
		return p.warnText
	}
	return p.badText
}

// pushFace gives a plain ui.Button the face of a macOS push button
// (NSButton) instead of MyGo's outlined one: in light, a hairline at 10%
// with a 0.5 pt contact shadow (none while pressed); in dark, a face one
// step above the controls', no outline, a top highlight and a contact
// shadow. Other platforms keep MyGo's look. Use it on every plain button
// that sits on a page; primary buttons keep theirs.
func pushFace(c *ui.Context, b ui.Element) ui.Element {
	return pushFaceOf(c, b, b)
}

// pushFaceOf is pushFace for a face drawn inside the control st, whose
// press, hover and disabled state it shows (pushMenu).
func pushFaceOf(c *ui.Context, b, st ui.Element) ui.Element {
	if inToolbar {
		return toolbarFaceOf(c, b, st)
	}
	if !plat.pushFace {
		return b
	}
	t := c.Theme()
	pressed := st.Pressed()
	if t.Dark {
		face := ui.Hex("#4a4a4c")
		if pressed {
			face = ui.Hex("#5a5a5c")
		}
		b.Background(face).BorderWidth(0).
			InsetShadow(0, 0.5, 0, 0, ui.RGBA(255, 255, 255, 0.10)).
			Shadow(0, 0.5, 1, 0, ui.RGBA(0, 0, 0, 0.30))
		return b
	}
	b.Border(1, ui.RGBA(0, 0, 0, 0.10))
	if !pressed {
		b.Shadow(0, 0.5, 1, 0, ui.RGBA(0, 0, 0, 0.12))
	}
	return b
}

// toolbarFace makes b a toolbar capsule: toolbarItemH tall, fully
// rounded, faceless at rest, a faint fill of the same shape on hover and
// press, and the glass pane behind it exactly its box.
func toolbarFace(c *ui.Context, b ui.Element) ui.Element {
	return toolbarFaceOf(c, b, b)
}

// toolbarFaceOf is toolbarFace for a face drawn inside the control st.
func toolbarFaceOf(c *ui.Context, b, st ui.Element) ui.Element {
	t := c.Theme()
	b.Height(toolbarItemH).Padding(0, toolbarPadX).Radius(toolbarItemH / 2).
		AlignItems(ui.Center).Justify(ui.Center).BorderWidth(0).Background(ui.Transparent).Shrink(0)
	if !st.IsDisabled() {
		switch {
		case st.Pressed() || toolbarForce == "pressed":
			b.Background(t.Text.Alpha(0.12))
		case st.Hovered() || toolbarForce == "hover":
			b.Background(t.Text.Alpha(0.07))
		}
	}
	if toolbarGlass != nil {
		toolbarGlass.Pane(b)
	}
	return b
}

// segmented is ui.Segmented that fits where it stands. In the Liquid
// Glass toolbar it is a capsule (toolbarFace's height and radius) with
// no track of its own: the chosen segment is a pill concentric with the
// capsule, the others a fainter pill on hover, as Finder's view switcher.
// Elsewhere it is NSSegmentedControl's (pageSegmented). Use it for every
// segmented control.
func segmented(c *ui.Context, selected *int, labels ...string) ui.Element {
	if !inToolbar {
		return pageSegmented(c, selected, labels...)
	}
	t := c.Theme()
	parts := ui.SegmentedBase(c, selected, len(labels))
	track := parts.Track.Height(toolbarItemH).Padding(toolbarSegPad).Gap(2).
		Radius(toolbarItemH / 2).AlignItems(ui.Stretch).Shrink(0)
	inner := float32(toolbarItemH/2 - toolbarSegPad)
	track.Children(func() {
		for i, label := range labels {
			s := parts.Segment(i).Padding(0, toolbarPadX-toolbarSegPad).Radius(inner)
			switch {
			case i == *selected:
				s.Background(t.Text.Alpha(0.12))
			case s.Pressed() || toolbarForce == "pressed":
				s.Background(t.Text.Alpha(0.10))
			case s.Hovered() || toolbarForce == "hover":
				s.Background(t.Text.Alpha(0.06))
			}
			s.Children(func() { ui.Text(c, label).SingleLine() })
		}
	})
	if toolbarGlass != nil {
		toolbarGlass.Pane(track)
	}
	return track
}

// pageSegmented is a segmented control on a page, as AppKit draws one
// (unslop R-14): a faint track with no outline, the chosen segment a
// raised face (white with a hairline and a contact shadow in light, a
// gray step up in dark), the others bare with a faint fill on hover.
// MyGo's default (chosen segment white on a near-white track, no shadow)
// left the state barely visible in light.
func pageSegmented(c *ui.Context, selected *int, labels ...string) ui.Element {
	t := c.Theme()
	track, face, hover := ui.RGBA(0, 0, 0, 0.06), ui.Hex("#ffffff"), ui.RGBA(0, 0, 0, 0.05)
	if t.Dark {
		track, face, hover = ui.RGBA(255, 255, 255, 0.06), ui.Hex("#636366"), ui.RGBA(255, 255, 255, 0.06)
	}
	const pad = 2
	inner := max(plat.controlRadius-pad, 2)
	parts := ui.SegmentedBase(c, selected, len(labels))
	g := parts.Track.Padding(pad).Gap(pad).Radius(plat.controlRadius).Background(track).AlignItems(ui.Stretch)
	g.Children(func() {
		for i, label := range labels {
			s := parts.Segment(i).Padding(2, 10).Radius(inner).AlignItems(ui.Center).Justify(ui.Center).
				Transition(colorMotion)
			switch {
			case i == *selected:
				s.Background(face).Shadow(0, 0.5, 1, 0, ui.RGBA(0, 0, 0, 0.15))
				if t.Dark {
					s.InsetShadow(0, 0.5, 0, 0, ui.RGBA(255, 255, 255, 0.10))
				} else {
					s.Border(1, ui.RGBA(0, 0, 0, 0.06))
				}
			case !s.IsDisabled() && (s.Hovered() || s.Pressed()):
				s.Background(hover)
			}
			s.Children(func() { ui.Text(c, label).SingleLine() })
		}
	})
	return g
}

// primaryButton is ui.PrimaryButton that looks right while disabled
// (unslop R-13): a disabled main action takes the push button's face,
// dimmed, and its label in the secondary color, not a washed-out accent
// (which reads as a broken color, ~2.4:1). The accent comes back when it
// is enabled. Set disabled here, not with Disabled after.
func primaryButton(c *ui.Context, label string, disabled bool) ui.Element {
	if !disabled {
		return ui.PrimaryButton(c, label)
	}
	return disabledPrimaryFace(c, ui.Button(c, label).Disabled(true))
}

// disabledPrimaryFace gives b, a button built as primary or plain that is
// disabled, primaryButton's disabled face; enabled buttons are left as
// they are. For buttons built with their own children (iconButton,
// opButton, busyButton): call it after Disabled.
func disabledPrimaryFace(c *ui.Context, b ui.Element) ui.Element {
	if !b.IsDisabled() {
		return b
	}
	t := c.Theme()
	if t.Dark {
		b.Background(ui.Hex("#4a4a4c").Alpha(0.5)).BorderWidth(0)
	} else {
		b.Background(ui.Hex("#ffffff")).Border(1, ui.RGBA(0, 0, 0, 0.10))
	}
	return b.TextColor(t.TextMuted).DimDisabled(false)
}

// disabledWhy builds a control that may be disabled with the reason as
// its tooltip while it is (Vibe Design Tips 020: a disabled control says
// why, as 需要后台助手). A disabled control shows no tooltip of its own,
// so the reason sits on a wrapper the size of the control.
//
//	disabledWhy(c, !canWrite, T.NeedsHelper, func() ui.Element {
//		return pushButton(c, T.Restart)
//	})
func disabledWhy(c *ui.Context, disabled bool, reason string, control func() ui.Element) ui.Element {
	var e ui.Element
	w := ui.Row(c).Shrink(0).AlignItems(ui.Center)
	if disabled && reason != "" {
		w.Tooltip(reason)
	}
	w.Children(func() { e = control().Disabled(disabled) })
	return e
}

// pushButton is a plain button with the native push-button face.
func pushButton(c *ui.Context, label string) ui.Element {
	return pushFace(c, ui.Button(c, label))
}

// pushMenu is a pull-down button (操作 ▾) with the same push-button face
// as pushButton, so a menu and a button side by side, or in the headings
// of neighbouring cards, look alike. Use it for every ui.MenuButton on a
// page (TestMenuButtonsHavePushFace).
//
// It is not ui.MenuButton: the button is a host carrying the menu, whose
// box reaches plat.menuDrop below the face it holds (menuBelow), so that
// the native menu opens just under the face instead of over its lower
// part. The host shows nothing itself and takes the pointer only on the
// face; it rings the face for the keyboard focus.
func pushMenu(c *ui.Context, label string, items func(m *ui.Menu)) ui.Element {
	t := c.Theme()
	host := menuBelow(ui.ButtonBase(c).Label(label).FocusRing(false).Menu(items))
	host.Children(func() {
		face := ui.Row(c).AlignItems(ui.Center).Padding(t.Space(1.5), t.Space(3.5)).Gap(t.Space(1.5)).
			Radius(t.Radius).Border(1, t.Border).TextColor(t.Text).Background(t.Surface)
		if !host.IsDisabled() {
			switch {
			case host.Pressed():
				face.Background(t.SurfacePressed)
			case host.Hovered():
				face.Background(t.SurfaceHover)
			}
		}
		pushFaceOf(c, face, host)
		if host.FocusVisible() {
			face.Draw(func(p *ui.Painter, r ui.Rect) {
				rad := t.Radius
				if inToolbar {
					rad = toolbarItemH / 2
				}
				p.FocusRing(r, [4]float32{rad, rad, rad, rad})
			})
		}
		face.Children(func() {
			ui.Text(c, label).SingleLine()
			menuChevron(c, t.TextMuted)
		})
	})
	return host
}

// menuChevron is the ▾ of a pull-down button, as ui.MenuButton draws it.
func menuChevron(c *ui.Context, color ui.Color) {
	t := c.Theme()
	ui.Box(c).Size(t.Space(2.5), t.Space(2.5)).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		var path ui.Path
		path.MoveTo(r.X+r.W*0.1, r.Y+r.H*0.3).LineTo(r.X+r.W*0.5, r.Y+r.H*0.7).LineTo(r.X+r.W*0.9, r.Y+r.H*0.3)
		p.StrokePath(&path, 1.5, color)
	})
}

// menuBelow makes e, an element with a native menu (Element.Menu), open
// that menu just below what it shows, as a macOS pull-down button does.
//
// MyGo pops the menu up at the element's bottom-left corner
// (popUpMenuPositioningItem:nil atLocation:), and AppKit puts the first
// item there: the menu's top edge lands 5 pt higher, over the element.
// e's box therefore reaches plat.menuDrop further down (bottom padding,
// cancelled by a negative bottom margin, so that the layout around it does
// not move) and takes the pointer only above that strip. Whatever e shows
// must be drawn by a child, not by e's own background or border, and
// e's margin must not be set after this.
func menuBelow(e ui.Element) ui.Element {
	d := plat.menuDrop
	if d == 0 {
		return e
	}
	return e.Padding(0, 0, d, 0).Margin(0, 0, -d, 0).HitTest(func(_, y float32, r ui.Rect) bool {
		return y < r.Y+r.H-d
	})
}

// toolIcon is a small faceless icon button for an action repeated on
// every row (测速 on a group, moving a layer): about 24 × 24, the muted
// icon, a faint fill on hover and press, its name as tooltip and for
// assistive technology. While busy (after busyDelay, for busyMin) the
// refresh icon turns, any other becomes a spinner (busyIcon); the button
// keeps its size.
func toolIcon(c *ui.Context, icon *ui.SVG, label string, busy bool) ui.Element {
	t := c.Theme()
	b := ui.Button(c, "").Label(label).Tooltip(label).Padding(5, 5).Size(24, 24).
		Background(ui.Transparent).BorderWidth(0).Radius(plat.controlRadius).Shrink(0)
	switch {
	case b.Pressed():
		b.Background(t.Text.Alpha(0.12))
	case b.Hovered():
		b.Background(t.Text.Alpha(0.07))
	}
	spin := showBusy(c, b, busy)
	b.Busy(spin).Children(func() {
		busyIcon(c, icon, spin).TextColor(t.TextMuted)
	})
	return b
}

// codeFrame is the frame of an editor or a diff view: the inner radius and
// a hairline, as code and evidence blocks have. The editor's own element
// does not draw a radius or a border, so the frame clips it.
func codeFrame(c *ui.Context, height float32, view func()) ui.Element {
	return ui.Column(c).Height(height).Radius(plat.innerRadius).Border(1, pal(c.Theme()).groupLine).Clip().Children(view)
}

// The diff block (unslop R-38): Twinnel's second moment, what a change does
// to the config, drawn as real lines on their own code surface. The
// takeover verdict, 订阅与覆写 → 预览 and 历史 → 与当前对比 use the same
// block, so the moment repeats: pal.code under the inner radius, 12 pt
// mono at an 18 pt line, + lines in okText and − lines in badText (the
// only rich color in the app besides the chart), context lines in the
// text color; the counts above it small, muted and mono.

// diffLine is one line of a diff: op is '+', '-' or ' ' (context).
type diffLine struct {
	op   byte
	text string
}

// diffLines splits a unified diff's body (lines starting with +, - or a
// space; ---/+++ headers and @@ hunks are left out) into diffLines.
func diffLines(text string) []diffLine {
	var out []diffLine
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"), strings.HasPrefix(l, "@@"):
		case strings.HasPrefix(l, "+"):
			out = append(out, diffLine{'+', l[1:]})
		case strings.HasPrefix(l, "-"):
			out = append(out, diffLine{'-', l[1:]})
		default:
			out = append(out, diffLine{' ', strings.TrimPrefix(l, " ")})
		}
	}
	return out
}

// diffLineH is a diff line's height: 18 pt for 12 pt mono.
const diffLineH = 18

// diffCounts is a diff's counts line ("新增 3 行 · 删除 1 行"): small,
// muted, mono figures.
func diffCounts(c *ui.Context, add, del int) ui.Element {
	return mono(ui.Text(c, i18n.F(i18n.T.DiffStats, add, del))).FontSize(plat.smallSize).
		TextColor(c.Theme().TextMuted).SingleLine()
}

// diffBlock draws lines as the diff block, its counts above it when
// counts is true. Every line wraps (never cut) and the block is
// selectable line by line.
func diffBlock(c *ui.Context, lines []diffLine, counts bool) ui.Element {
	t := c.Theme()
	p := pal(t)
	add, del := 0, 0
	for _, l := range lines {
		switch l.op {
		case '+':
			add++
		case '-':
			del++
		}
	}
	return ui.Column(c).Gap(6).Children(func() {
		if counts {
			diffCounts(c, add, del)
		}
		ui.Column(c).Padding(8, 0).Radius(plat.innerRadius).Background(p.code).Border(1, p.groupLine).Clip().Children(func() {
			for i, l := range lines {
				mark, col, fill := " ", t.Text, ui.Transparent
				switch l.op {
				case '+':
					mark, col, fill = "+", p.okText.Alpha(0.9), t.Success.Alpha(0.08)
				case '-':
					mark, col, fill = "−", p.badText, t.Danger.Alpha(0.08)
				}
				ui.Row(c.Key(i)).Gap(8).PaddingX(12).Background(fill).AlignItems(ui.Start).Children(func() {
					mono(ui.Text(c, mark)).FontSize(plat.calloutSize).TextColor(col).Width(8).Shrink(0).
						FixedLineHeight(diffLineH).AccessHidden(true)
					mono(ui.Text(c, l.text)).FontSize(plat.calloutSize).TextColor(col).Grow(1).Shrink(1).
						Wrap().FixedLineHeight(diffLineH).Selectable()
				})
			}
		})
	})
}
