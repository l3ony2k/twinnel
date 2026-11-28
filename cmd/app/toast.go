package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// Twinnel's toasts (unslop R-12, kubai R-5): the outcome of what the person
// just did, at the bottom centre of the page pane, as a macOS HUD. One
// look in both appearances, never the inverted pill MyGo draws by default:
//
//   - a capsule 36 tall on the control surface (dark #3a3a3c with a top
//     highlight, light white), a hairline and a soft shadow;
//   - a 16 pt mark first: the check in okText for a success, the error
//     glyph in badText for a failure, none for plain information;
//   - one line of body text, then the action in the accent, then ✕ on
//     every failure and every toast that stays until it is closed, so a
//     persistent error never traps the person on 查看;
//   - it rises 8 pt and fades in over 240 ms (easeOut) and sinks 4 pt as
//     it fades out over 160 ms (easeIn); Reduce Motion fades only.
//
// On Windows the toast is a Fluent overlay instead: radius 8 (the
// overlay corner), WinUI's flyout surface, at the bottom right of the
// page pane, and one at a time: a new toast replaces the one showing
// (kubai 016), where macOS stacks them as HUDs.
//
// Types: toastSuccess, toastError, or "" (information). A toast without
// a type whose text starts with i18n.T.ErrorPrefix is an error, the
// prefix dropped (the mark says it).
const (
	toastSuccess = "success"
	toastError   = "error"
)

// Toast geometry.
const (
	toastH      = 36
	toastMaxW   = 480
	toastBottom = 16 // above the pane's bottom edge
	toastClose  = 20 // the ✕ button's side
)

// toastMotion is how a toast comes and goes.
var toastMotion = ui.ElementTransition{
	Duration: durMedium, Ease: easeOut,
	ExitDuration: 160 * time.Millisecond, ExitEase: easeIn,
	Enter: &ui.Motion{Opacity: 0, Y: 8, Scale: 0.96},
	Exit:  &ui.Motion{Opacity: 0, Y: 4},
}

// toastSurface is a toast's fill, hairline and shadow color.
func toastSurface(t *ui.Theme) (fill, line, shadow ui.Color) {
	if plat.fluent {
		if t.Dark {
			return ui.Hex("#2c2c2c"), ui.RGBA(0, 0, 0, 0.2), ui.RGBA(0, 0, 0, 0.26)
		}
		return ui.Hex("#fcfcfc"), ui.RGBA(0, 0, 0, 0.0578), ui.RGBA(0, 0, 0, 0.14)
	}
	if t.Dark {
		return ui.Hex("#3a3a3c"), ui.RGBA(255, 255, 255, 0.10), ui.RGBA(0, 0, 0, 0.35)
	}
	return ui.Hex("#ffffff"), ui.RGBA(0, 0, 0, 0.08), ui.RGBA(0, 0, 0, 0.12)
}

// toastKind is the toast's type and the text to show: a typeless toast
// that starts with the error prefix is an error without it.
func toastKind(ts ui.Toast) (kind, text string) {
	kind, text = ts.Type, ts.Title
	if p := i18n.T.ErrorPrefix; kind == "" && p != "" && strings.HasPrefix(text, p) {
		kind, text = toastError, strings.TrimPrefix(text, p)
	}
	return kind, text
}

// toasts builds the window's toasts with Twinnel's look. Call once per frame
// of the main window (mainView), after the page.
func (a *app) toasts(c *ui.Context) {
	left := a.side.widthOf(a.side.mode) // centred on the page pane, not the window
	var replaced []string
	ui.ToastViewportBase(c, func(viewport ui.Element, list []ui.Toast) {
		viewport.Padding(0, 16, toastBottom, left+16).AlignItems(ui.Center).Gap(8)
		if plat.fluent {
			viewport.Padding(0, 24, 24, left+24).AlignItems(ui.End)
		}
		show, drop := toastsShown(list, plat.fluent)
		replaced = drop
		for _, ts := range show {
			toastView(c, ts)
		}
	})
	for _, id := range replaced { // after the viewport: closing edits its list
		c.CloseToast(id)
	}
}

// toastsShown splits the toasts into those to show and those a newer one
// replaced: with one (Windows) only the newest shows and the others are
// to be closed; else every one shows, stacked (macOS, Linux).
func toastsShown(list []ui.Toast, one bool) (show []ui.Toast, replaced []string) {
	if !one || len(list) <= 1 {
		return list, nil
	}
	for _, ts := range list[:len(list)-1] {
		replaced = append(replaced, ts.ID)
	}
	return list[len(list)-1:], replaced
}

// toastView draws one toast.
func toastView(c *ui.Context, ts ui.Toast) {
	t := c.Theme()
	p := pal(t)
	kind, text := toastKind(ts)
	fill, line, shadow := toastSurface(t)
	parts := ui.ToastBase(c, ts)
	root := parts.Root.Row().AlignItems(ui.Center).Gap(8).MinHeight(toastH).MaxWidth(toastMaxW).
		Padding(4, 14, 4, 12).Radius(toastH/2).Background(fill).Border(1, line).
		Shadow(0, 8, 24, 0, shadow).Transition(toastMotion)
	if plat.fluent {
		root.Radius(8).Shadow(0, 8, 16, 0, shadow)
	} else if t.Dark {
		root.InsetShadow(0, 0.5, 0, 0, ui.RGBA(255, 255, 255, 0.08))
	}
	closable := kind == toastError || ts.Timeout < 0
	if closable {
		root.PaddingEnd(6)
	}
	root.Children(func() {
		switch kind {
		case toastSuccess:
			ui.Icon(c, iconOK).FontSize(iconNav).TextColor(p.okText).Shrink(0).AccessHidden(true)
		case toastError:
			ui.Icon(c, iconError).FontSize(iconNav).TextColor(p.badText).Shrink(0).AccessHidden(true)
		}
		ui.Text(c, text).TextColor(t.Text).SingleLine().Shrink(1).Tooltip(text)
		if ts.Action != "" {
			b := parts.ActionButton().Padding(2, 6).Radius(plat.controlRadius).Shrink(0)
			switch {
			case b.Pressed():
				b.Background(t.Text.Alpha(0.12))
			case b.Hovered():
				b.Background(t.Text.Alpha(0.07))
			}
			b.Children(func() { ui.Text(c, ts.Action).TextColor(t.Accent).FontWeight(500).SingleLine() })
		}
		if closable {
			b := parts.CloseButton().Label(i18n.T.Close).Tooltip(i18n.T.Close).
				Size(toastClose, toastClose).Radius(toastCloseRadius()).AlignItems(ui.Center).Justify(ui.Center).Shrink(0)
			switch {
			case b.Pressed():
				b.Background(t.Text.Alpha(0.14))
			case b.Hovered():
				b.Background(t.Text.Alpha(0.08))
			}
			b.Children(func() { ui.Icon(c, iconClose).FontSize(iconSmall).TextColor(t.TextMuted) })
		}
	})
}

// flushToast shows the pending outcome message as a toast: a success
// that needs no answer goes away by itself (errors stay on the page); a
// failed operation stays until closed, with 查看 and ✕.
func (a *app) flushToast(c *ui.Context) {
	if a.toast != "" {
		ts := ui.Toast{ID: a.toast, Title: a.toast, Type: toastSuccess}
		if a.toastOp {
			ts.Timeout, ts.Action, ts.OnAction = 8*time.Second, i18n.T.EvShow, a.showTwinnelLog
		}
		c.AddToast(ts)
		a.toast, a.toastOp = "", false
	}
	if a.toastHint != "" {
		c.AddToast(ui.Toast{ID: a.toastHint, Title: a.toastHint, Timeout: 10 * time.Second})
		a.toastHint = ""
	}
	if a.toastErr != "" {
		c.AddToast(ui.Toast{ID: "op-failed", Title: a.toastErr, Type: toastError, Timeout: -1, Action: i18n.T.EvShow, OnAction: a.showTwinnelLog})
		a.toastErr = ""
	}
}

// toastCloseRadius is the ✕ button's corner: round in the capsule, the
// control radius in Fluent's rounded rectangle.
func toastCloseRadius() float32 {
	if plat.fluent {
		return plat.controlRadius
	}
	return toastClose / 2
}
