package main

import (
	"time"

	"github.com/egoist/mygo"
)

// sidebarCollapseShots captures the sidebar's three modes (expanded, icons
// only, hidden) on 总览 and 健康检查 (whose row has a count to look at),
// DUET_SHOTS_FLOW=sidecollapse. The window is the hidden shot window.
func (a *app) sidebarCollapseShots(save func(*mygo.Window, string)) {
	yes := true
	for _, m := range []struct {
		name string
		mode sidebarMode
	}{{"expanded", sideExpanded}, {"icons", sideRail}, {"hidden", sideHidden}} {
		for _, page := range []string{pageOverview, pageHealth} {
			mygo.RunOnMain(func() {
				a.side.load(a.cfg)
				a.side.setMode(m.mode)
				old := a.page
				a.page = page
				a.pageChanged(old)
				a.shotNav.active = &yes
				a.win.Invalidate()
			})
			time.Sleep(700 * time.Millisecond)
			save(a.win, "side-"+m.name+"-"+page)
		}
	}
}
