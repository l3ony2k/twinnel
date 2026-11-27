package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
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
	gapRow     = 8  // between rows inside a group (System Settings' rhythm)
	gapGroup   = 20 // between groups on a page
	groupPadX  = 16 // a group's side padding; its heading lines up with it
	groupPadY  = 12
	labelWidth = 96 // the label column of kv rows
	gapSub     = 2  // between a line and the secondary line under it
)

// lineText is the line height of text that wraps, as a multiple of its
// size: every paragraph (hints, notes, errors, row details) spaces its
// lines alike, CJK and Latin mixed, whatever font the fallback picks.
const lineText = 1.4

// Icon sizes: one per role.
const (
	iconNav    = 16 // sidebar
	iconInline = 14 // in buttons, headings, notices, rows
	iconSmall  = 12 // disclosure chevrons, tiny keys
	iconLarge  = 22 // the verdict and empty states
	iconHero   = 28 // the app mark on the welcome page
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
	d := ui.Row(c).Gap(4).AlignItems(ui.Center).Cursor(ui.CursorPointer).
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

// Busy feedback (vibedesign 017): work shows on the control that started
// it, only once it took longer than busyDelay, and then for at least
// busyMin, so a quick answer never flashes a spinner.
const (
	busyDelay = 300 * time.Millisecond
	busyMin   = 500 * time.Millisecond
)

type busyState struct {
	since time.Time // when the work began; zero while idle
	shown time.Time // when the spinner appeared; zero while hidden
}

// showBusy says whether e should show a spinner now for work that is
// running when busy is true; e keeps the timing (ui.Local), so each
// control has its own.
func showBusy(c *ui.Context, e ui.Element, busy bool) bool {
	st := ui.Local(e, "duet.busy", func() *busyState { return &busyState{} })
	if st == nil {
		return false
	}
	b, now := *st, c.Now()
	switch {
	case busy && b.since.IsZero():
		b.since = now
	case !busy:
		b.since = time.Time{}
	}
	if b.shown.IsZero() && busy && now.Sub(b.since) >= busyDelay {
		b.shown = now
	}
	if !b.shown.IsZero() && !busy && now.Sub(b.shown) >= busyMin {
		b.shown = time.Time{}
	}
	switch {
	case busy && b.shown.IsZero():
		c.After(busyDelay - now.Sub(b.since))
	case !busy && !b.shown.IsZero():
		c.After(busyMin - now.Sub(b.shown))
	}
	*st = b
	return !b.shown.IsZero()
}

// spinnerIcon is a spinner the size of an inline icon, to stand in for
// one while its control works.
func spinnerIcon(c *ui.Context) ui.Element {
	return ui.Spinner(c).Size(iconInline, iconInline)
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

// kvWith draws a label and custom content on one row.
func kvWith(c *ui.Context, label string, body func()) {
	t := c.Theme()
	ui.Row(c).Gap(12).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Children(func() {
		ui.Text(c, label).TextColor(t.TextMuted).Width(labelWidth).Shrink(0).SingleLine()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Children(body)
	})
}

// kvControl fits a control (a select, a field) into a kv row: the row's
// height, so a row holding one lines up with its plain neighbours, and
// no wider than a value needs.
func kvControl(e ui.Element) ui.Element {
	return e.Height(plat.rowMinHeight).Grow(1).MaxWidth(260)
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

// card is a group of the main window, as System Settings draws them: the
// heading above a quiet box. An empty title draws only the box.
func card(c *ui.Context, title string, body func()) ui.Element {
	return cardWith(c, title, nil, body)
}

// cardWith is card with something at the right of the heading (a button,
// a count).
func cardWith(c *ui.Context, title string, trailing func(), body func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(8).Children(func() {
		if title != "" || trailing != nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(groupPadX).Height(28).Children(func() {
				ui.Text(c, title).FontSize(plat.headingSize).FontWeight(plat.headingWeight).SingleLine()
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
		ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(groupPadX).MinHeight(28).Children(head)
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
func listBox(c *ui.Context, t *ui.Theme) ui.Element {
	return groupBox(c, t).Padding(4, groupPadX).Gap(1).Dividers(1, pal(t).separator)
}

// listCard is a group whose rows are split by hairlines (listBox), with
// a heading row above it built by head (icon, title, actions); nil head
// draws only the list. Each row is a listRow.
func listCard(c *ui.Context, head func(), body func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(8).Children(func() {
		if head != nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(groupPadX).MinHeight(28).Children(head)
		}
		listBox(c, t).Children(body)
	})
}

// listHead is the usual heading of a listCard: an optional line icon,
// the title, then trailing (buttons) at the right.
func listHead(c *ui.Context, icon *ui.SVG, title string, trailing func()) func() {
	return func() {
		t := c.Theme()
		if icon != nil {
			ui.Icon(c, icon).FontSize(iconInline).TextColor(t.TextMuted)
		}
		ui.Text(c, title).FontSize(plat.headingSize).FontWeight(plat.headingWeight).SingleLine()
		ui.Spacer(c)
		if trailing != nil {
			trailing()
		}
	}
}

// listRow is one row of a listCard: its lines stacked with the gap of a
// group's rows.
func listRow(c *ui.Context) ui.Element {
	return ui.Column(c).Gap(gapRow - 4).PaddingY(10)
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
// duet knows (i18n.PlainNetError: 没能连上 GitHub …) is said in words, the
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
// in the UI language when duet knows it (i18n.Warning).
func warningNote(c *ui.Context, w string) ui.Element {
	t := c.Theme()
	return ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(pal(t).warnText).MarginY(1)
		ui.Text(c, i18n.T.Warning(w)).FontSize(plat.calloutSize).Grow(1).Wrap().LineHeight(lineText).Selectable()
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
	if !plat.pushFace || inToolbar {
		return b
	}
	t := c.Theme()
	pressed := b.Pressed()
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

// segmented is ui.Segmented that fits where it stands: in the Liquid Glass
// toolbar its track is faceless (no fill, no outline), so only the chosen
// segment's fill shows on the capsule, as Finder's and System Settings'
// toolbar groups; elsewhere it keeps the theme's track.
func segmented(c *ui.Context, selected *int, labels ...string) ui.Element {
	e := ui.Segmented(c, selected, labels...)
	if inToolbar {
		e.Background(ui.Transparent).Border(0, ui.Transparent)
	}
	return e
}

// pushButton is a plain button with the native push-button face.
func pushButton(c *ui.Context, label string) ui.Element {
	return pushFace(c, ui.Button(c, label))
}

// toolIcon is a small faceless icon button for an action repeated on
// every row (测速 on a group, moving a layer): about 24 × 24, the muted
// icon, a faint fill on hover and press, its name as tooltip and for
// assistive technology. While busy (after busyDelay, for busyMin) the
// icon turns into a spinner in place; the button keeps its size.
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
	b.Children(func() {
		if spin {
			spinnerIcon(c)
			return
		}
		ui.Icon(c, icon).FontSize(iconInline).TextColor(t.TextMuted)
	})
	return b
}

// codeFrame is the frame of an editor or a diff view: the inner radius and
// a hairline, as code and evidence blocks have. The editor's own element
// does not draw a radius or a border, so the frame clips it.
func codeFrame(c *ui.Context, height float32, view func()) ui.Element {
	return ui.Column(c).Height(height).Radius(plat.innerRadius).Border(1, pal(c.Theme()).groupLine).Clip().Children(view)
}
