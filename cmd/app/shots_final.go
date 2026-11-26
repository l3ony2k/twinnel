package main

import (
	"time"

	"github.com/egoist/mygo"
)

// finalShots (DUET_SHOTS_FLOW=final) captures what ui-final added that the
// page walk does not show: a node tile with the keyboard focus (as ↓ moved
// it), the devices card's card-size empty state, the panel's state marks.
func (a *app) finalShots(save func(*mygo.Window, string)) {
	page := func(p string, fn func()) {
		mygo.RunOnMain(func() {
			old := a.page
			a.page = p
			a.pageChanged(old)
			if fn != nil {
				fn()
			}
			a.win.Invalidate()
		})
		time.Sleep(900 * time.Millisecond)
	}
	page(pageProxies, func() {
		s := a.mon.Snapshot()
		if p := s.Mihomo.Primary; p != nil && len(p.Members) > 1 {
			a.groupsOpen[p.Name] = true
			a.tileFocus = tileRef{p.Name, p.Members[min(len(p.Members)-1, 3)].Name}
		}
	})
	save(a.win, "final-proxies-tile-focus")
	page(pageTailscale, func() { a.shotNoPeers = true })
	save(a.win, "final-devices-empty")
	page(pageOverview, func() { a.shotNoPeers = false })
	mygo.RunOnMain(a.togglePanel)
	time.Sleep(1500 * time.Millisecond)
	var panel *mygo.Window
	mygo.RunOnMain(func() { panel = a.panel })
	if panel != nil {
		save(panel, "final-panel")
	}
}
