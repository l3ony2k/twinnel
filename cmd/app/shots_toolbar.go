package main

import (
	"time"

	"github.com/egoist/mygo"
)

// toolbarForce, set only by the toolbar shot flow, draws every toolbar
// capsule (and every unchosen toolbar segment) as hovered or pressed:
// "hover" or "pressed". Shots cannot move the pointer, and this is the
// same code path a real hover takes. Main thread only.
var toolbarForce string

// toolbarShots saves each page that has toolbar actions with its
// capsules idle, hovered and pressed (TWINNEL_SHOTS_FLOW=toolbar).
func (a *app) toolbarShots(save func(*mygo.Window, string)) {
	for _, p := range []string{pageProxies, pageTailscale, pageHealth, pageLogs} {
		for _, state := range []string{"", "hover", "pressed"} {
			mygo.RunOnMain(func() {
				old := a.page
				a.page = p
				a.pageChanged(old)
				toolbarForce = state
				a.win.Invalidate()
			})
			time.Sleep(900 * time.Millisecond)
			name := "toolbar-" + p
			if state != "" {
				name += "-" + state
			}
			save(a.win, name)
		}
	}
	mygo.RunOnMain(func() { toolbarForce = "" })
}
