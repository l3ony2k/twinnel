//go:build linux

package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/platform/linux"
)

// The tray panel on Linux. AppIndicator (StatusNotifierItem) tells neither
// where the icon is nor where it was clicked, and on Wayland an app cannot
// place a plain window. So the panel is a panel window (WindowOptions.Panel):
// on compositors with wlr-layer-shell (Hyprland, KDE Plasma, Sway) a layer
// surface MyGo anchors at the corner panelOrigin picks, out of the way of
// the bars' exclusive zones; on X11 a utility window there (or at the
// pointer); on GNOME Wayland, which places it itself, wherever Mutter puts
// it. It closes as it loses the keyboard, on Escape, and on a second
// activation of the icon.

// panelClosedAt is when the panel last closed. Main thread only.
var panelClosedAt time.Time

// panelReopenGap: a click on the tray icon first takes the keyboard from
// the open panel, which closes it, then activates the icon. An activation
// this soon after the close is that click: it must not open it again.
const panelReopenGap = 400 * time.Millisecond

// panelStartH is the panel's height until its content is measured
// (fitShotPanel resizes it).
const panelStartH = 420

func (a *app) openPanelWindow() {
	if time.Since(panelClosedAt) < panelReopenGap {
		return
	}
	pos := os.Getenv("TWINNEL_PANEL_POSITION")
	if pos == "" {
		pos = panelDefault(os.Getenv("XDG_CURRENT_DESKTOP"))
	}
	h := panelStartH
	if a.panelShotH > 0 {
		h = a.panelShotH
	}
	x, y := placePanel(panelWidth, h, pos)
	var p *mygo.Window
	p = mygo.NewWindow(mygo.WindowOptions{
		Title:         i18n.T.AppName,
		Width:         panelWidth,
		Height:        h,
		X:             x,
		Y:             y,
		Panel:         true,
		Frameless:     true,
		Transparent:   true, // panelView draws its rounded box
		DisableResize: true,
		Content: ui.View(func(c *ui.Context) {
			if c.Shortcut(0, ui.KeyEscape) {
				p.Close()
				return
			}
			a.panelView(c)
		}),
	})
	a.panel = p
	p.OnBlur(func() { p.Close() })
	p.OnResize(func() { // the content's height came in: keep a bottom corner on the bar
		_, wh := p.Size()
		if nx, ny := placePanel(panelWidth, wh, pos); nx != x || ny != y {
			x, y = nx, ny
			p.SetPosition(x, y)
		}
	})
	p.OnClosed(func() {
		if a.panel == p {
			a.panel = nil
		}
		panelClosedAt = time.Now()
	})
	p.Focus()
}

// placePanel is the panel's origin on the primary display for pos; the
// pointer counts on X11 only (Wayland gives none).
func placePanel(w, h int, pos string) (int, int) {
	d := mygo.Screen.PrimaryDisplay()
	wa := d.WorkArea
	if wa.Width == 0 {
		wa = d.Bounds
	}
	ptr := mygo.Point{}
	x11 := onX11()
	if x11 {
		ptr = mygo.Screen.CursorScreenPoint()
		wa = mygo.Screen.DisplayNearestPoint(ptr).WorkArea
	}
	return panelOrigin(rect{wa.X, wa.Y, wa.Width, wa.Height}, w, h, pos, ptr.X, ptr.Y, x11)
}

// onX11 reports whether GTK runs on X11 (GDK_BACKEND=x11, or no Wayland
// display), where windows go where the app puts them and the pointer is
// known.
func onX11() bool {
	if b := os.Getenv("GDK_BACKEND"); b != "" {
		return b == "x11"
	}
	return os.Getenv("WAYLAND_DISPLAY") == ""
}

// checkTrayHost asks the session bus whether a tray host runs (a
// StatusNotifierWatcher), a few times while the desktop's bar may still
// be starting (Twinnel can start before it at login). With none, the
// icon shows nowhere: the main window opens, says so once, and closing
// it quits Twinnel, which would otherwise run on with no way back in.
func (a *app) checkTrayHost() {
	go func() {
		run := func(ctx context.Context, argv []string) ([]byte, error) {
			return exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
		}
		var present, known bool
		for i := 0; i < 5; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			present, known = linux.TrayHostPresent(ctx, run)
			cancel()
			if present || !known {
				return
			}
			time.Sleep(2 * time.Second)
		}
		hint := linux.TrayHintFor(present, known, os.Getenv("XDG_CURRENT_DESKTOP"))
		log.Printf("tray: no StatusNotifierWatcher on the session bus (hint %d); opening the window", hint)
		mygo.RunOnMain(func() { a.trayless(hint) })
	}()
}

// trayMissing: the tray icon could not be made (no AppIndicator library).
func (a *app) trayMissing() { a.trayless(linux.TrayHintOther) }

// trayless opens the main window in place of an icon nobody can see,
// says why once, and makes closing the window quit Twinnel, which would
// otherwise run on with no way back in. Main thread.
func (a *app) trayless(hint linux.TrayHint) {
	msg := i18n.T.TrayHostMissing
	if hint == linux.TrayHintGNOME {
		msg = i18n.T.TrayHostMissingGNOME
	}
	a.openMain()
	a.toastErr = msg
	mygo.App.OnWindowAllClosed(func() { mygo.App.Quit() })
	a.invalidate()
}
