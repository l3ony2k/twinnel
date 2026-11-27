package main

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// sidebarCollapseShots captures the sidebar's three modes (expanded, icons
// only, hidden) on 总览, 总览 scrolled and 健康检查 (whose row has a count),
// TWINNEL_SHOTS_FLOW=sidecollapse. The window is the hidden shot window.
func (a *app) sidebarCollapseShots(save func(*mygo.Window, string)) {
	yes := true
	shotLights = true
	mygo.RunOnMain(func() { a.win.SetSize(a.winWidth(), 660) }) // the main window's size, so pages scroll
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
		// 总览 scrolled: the toolbar's scroll edge over the page, beside
		// the sidebar's material (one material per region, no corner).
		mygo.RunOnMain(func() {
			old := a.page
			a.page = pageOverview
			a.pageChanged(old)
			if st := a.pageScroll[pageOverview]; st != nil {
				st.Y = 240
			}
			a.win.Invalidate()
		})
		time.Sleep(700 * time.Millisecond)
		save(a.win, "side-"+m.name+"-overview-scrolled")
		mygo.RunOnMain(func() {
			if st := a.pageScroll[pageOverview]; st != nil {
				st.Y = 0
			}
		})
	}
}

// shotLights draws the traffic lights into the shot window (lightsOverlay):
// a hidden window's capture shows none, and the toolbar is placed by them.
var shotLights bool

// lightsOverlay is a calibration overlay of the traffic lights where the
// system draws them: from TitleBar() and the lights' measured size and
// pitch (sidebar.go). Shots only.
func lightsOverlay(c *ui.Context) {
	bar := c.TitleBar()
	if bar.Left == 0 {
		return
	}
	cols := []ui.Color{ui.Hex("#ff5f57"), ui.Hex("#febc2e"), ui.Hex("#28c840")}
	top := bar.Bottom - trafficLightD
	for i, col := range cols {
		left := bar.Left - trafficLightD - float32(2-i)*trafficLightPitch
		ui.Box(c).Absolute().Left(left).Top(top).Size(trafficLightD, trafficLightD).
			Radius(trafficLightD / 2).Background(col).PassThrough().AccessHidden(true)
	}
}
