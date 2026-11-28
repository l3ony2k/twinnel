package main

import (
	"time"

	"github.com/egoist/mygo"
)

// shotOffscreen is where a shot window shows on Windows: far off every
// display, so nothing of it is on screen.
const shotOffscreen = -32000

// shotShowOffscreen reports whether shot windows must be shown (off
// screen, inactive) to be captured. On Windows a hidden window gets no
// WM_PAINT after its first frame, so MyGo's CapturePage keeps returning
// that frame: every page came out the same. macOS and Linux render a
// hidden window's content on capture and stay hidden.
func shotShowOffscreen(goos string) bool { return goos == "windows" }

// showShotWindow moves a hidden shot window off screen and shows it
// without activating it (SW_SHOWNOACTIVATE), then waits for its first
// frames. It never takes the focus. A window already visible is left as
// it is.
func showShotWindow(w *mygo.Window) {
	shown := false
	mygo.RunOnMain(func() {
		if w.IsDestroyed() || w.IsVisible() {
			return
		}
		w.SetPosition(shotOffscreen, shotOffscreen)
		w.ShowInactive()
		w.Invalidate()
		shown = true
	})
	if shown {
		time.Sleep(600 * time.Millisecond)
	}
}
