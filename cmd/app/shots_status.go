package main

import (
	"os"
	"time"

	"github.com/egoist/mygo"
)

// statusShots (DUET_SHOTS_FLOW=status) captures the status surfaces: the
// overview, the health page (the accepted group open), Tailscale, 设置 and
// the flyout. DUET_SHOTS_ACCEPT=1 first accepts the exit node in use, in
// memory only (shot mode saves no config). Nothing is written.
func (a *app) statusShots(save func(*mygo.Window, string)) {
	if os.Getenv("DUET_SHOTS_ACCEPT") == "1" {
		mygo.RunOnMain(func() {
			if s := a.mon.Snapshot(); s.Tailscale.State.ExitNode != nil {
				a.acceptExitNode(s.Tailscale.State.ExitNode.Name)
			}
		})
		time.Sleep(500 * time.Millisecond)
	}
	mygo.RunOnMain(func() { a.win.SetSize(a.winWidth(), 1250) })
	for _, p := range []string{pageOverview, pageHealth, pageTailscale, pageSettings} {
		mygo.RunOnMain(func() {
			old := a.page
			a.page = p
			a.pageChanged(old)
			if p == pageHealth {
				a.healthOpen["caused-by"] = true
			}
			a.win.Invalidate()
		})
		time.Sleep(1500 * time.Millisecond)
		save(a.win, "main-"+p)
	}
	mygo.RunOnMain(a.togglePanel)
	time.Sleep(1500 * time.Millisecond)
	var panel *mygo.Window
	mygo.RunOnMain(func() { panel = a.panel })
	if panel != nil {
		save(panel, "panel")
	}
}
