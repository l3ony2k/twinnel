package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo"
)

// centerShots (DUET_SHOTS_FLOW=center) captures pages at widths from a
// small window to a full-screen wide display, with the sidebar expanded,
// as icons and hidden: past pageMaxWidth the page column, and the
// toolbar's title and actions over it, sit in the middle of the pane.
// Run with DUET_LANG=zh|en and DUET_THEME=light|dark.
func (a *app) centerShots(save func(*mygo.Window, string)) {
	yes := true
	shotLights = true
	mygo.RunOnMain(func() {
		if len(a.events.evs) == 0 {
			a.recordEvents(shotEvents(time.Now()))
		}
	})
	show := func(w int, mode sidebarMode, page string) {
		mygo.RunOnMain(func() {
			a.win.SetSize(w, 900)
			a.side.load(a.cfg)
			a.side.setMode(mode)
			old := a.page
			a.page = page
			a.pageChanged(old)
			a.shotNav.active = &yes
			a.win.Invalidate()
		})
		time.Sleep(900 * time.Millisecond)
		// The column centers with the width the frame before measured,
		// and the overview lays out on its own measure: let both settle.
		for range 3 {
			save(a.win, fmt.Sprintf("center-%s-%d-%s", page, w, [...]string{"expanded", "icons", "hidden"}[mode]))
			time.Sleep(250 * time.Millisecond)
		}
	}
	for _, w := range []int{900, 1280, 1600, 2560, 3008, 3456} {
		for _, page := range []string{pageOverview, pageHealth, pageSettings, pageLogs} {
			show(w, sideExpanded, page)
		}
	}
	for _, w := range []int{1600, 3456} {
		for _, mode := range []sidebarMode{sideRail, sideHidden} {
			for _, page := range []string{pageOverview, pageSettings} {
				show(w, mode, page)
			}
		}
	}
}
