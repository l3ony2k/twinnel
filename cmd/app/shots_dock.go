package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/status"
)

// dockShots (DUET_SHOTS_FLOW=dock) captures 设置 → 通用 with each value of
// 在程序坞中显示. The setting changes in memory only, and shot mode never
// changes the activation policy (dockManaged).
func (a *app) dockShots(save func(*mygo.Window, string)) {
	mygo.RunOnMain(func() {
		a.win.SetSize(a.winWidth(), 900)
		old := a.page
		a.page = pageSettings
		a.pageChanged(old)
	})
	for _, st := range []struct{ name, v string }{
		{"settings-dock-whenopen", ""},
		{"settings-dock-always", status.DockAlways},
		{"settings-dock-never", status.DockNever},
	} {
		mygo.RunOnMain(func() {
			a.cfg.Dock = st.v
			a.win.Invalidate()
		})
		time.Sleep(1200 * time.Millisecond)
		save(a.win, st.name)
	}
}
