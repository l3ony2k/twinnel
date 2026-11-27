package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/status"
)

// trayspeedShots (DUET_SHOTS_FLOW=trayspeed) captures 设置 with the menu
// bar card in each state: one line, two lines, off. The settings change in
// memory only (shot mode saves no config). The menu bar item itself cannot
// be captured without showing it: TestTraySpeedShots draws it instead.
func (a *app) trayspeedShots(save func(*mygo.Window, string)) {
	mygo.RunOnMain(func() {
		a.win.SetSize(a.winWidth(), 1400)
		old := a.page
		a.page = pageSettings
		a.pageChanged(old)
	})
	for _, st := range []struct {
		name   string
		off    bool
		layout string
	}{
		{"settings-trayspeed-one", false, ""},
		{"settings-trayspeed-two", false, status.TraySpeedTwoLines},
		{"settings-trayspeed-off", true, status.TraySpeedTwoLines},
	} {
		mygo.RunOnMain(func() {
			a.cfg.TraySpeedOff, a.cfg.TraySpeedLayout = st.off, st.layout
			a.win.Invalidate()
		})
		time.Sleep(1200 * time.Millisecond)
		save(a.win, st.name)
	}
}
