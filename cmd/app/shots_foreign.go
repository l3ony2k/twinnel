package main

import (
	"context"
	"strconv"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// foreignShots (TWINNEL_SHOTS_FLOW=foreign) captures the state where
// another program's mihomo holds the controller: the 总览 (banner, mihomo
// card with what holds it and the two ways out, tiles and chart without
// the other program's values, the sidebar's ⊖ mark) at two widths, the 节点
// page and the flyout. The state is the live (fake) snapshot with the
// helper's owner and the controller-owner row overridden in memory
// (shotSnap): synthetic values only, nothing is written. Run with
// TWINNEL_LANG=zh-Hans|en and TWINNEL_THEME=light|dark.
func (a *app) foreignShots(save shotSaver) {
	owner := &foreign.Owner{Addr: "127.0.0.1:19894", Verdict: foreign.Foreign, Via: foreign.ViaPort, PID: 4242,
		Exe: shotForeignExe, Parent: shotForeignParent, At: time.Now()}
	row := shotOwnerRow(owner)
	mygo.RunOnMain(func() {
		a.shotSnap = func(s *status.Snapshot) {
			s.Helper = status.Helper{State: status.HelperConnected, Status: &rpc.Status{HelperVersion: "0.1.0",
				LastApply: &rpc.ApplyResult{Time: time.Now().Add(-2 * time.Hour), Written: true, Reloaded: true},
				Mihomo:    rpc.MihomoStatus{Owner: owner}}}
			s.WritesAllowed = true // show the fixes as they would be offered; nothing is clicked
			s.Loaded = true
			rep := &health.Report{Platform: reportPlatform(*s), Started: time.Now(), Controller: owner}
			if old := s.Health.Report; old != nil {
				cp := *old
				rep = &cp
				rep.Controller = owner
				rep.Checks = nil
				for _, cr := range old.Checks {
					if cr.ID != row.ID {
						rep.Checks = append(rep.Checks, cr)
					}
				}
			}
			rep.Checks = append([]health.CheckReport{row}, rep.Checks...)
			s.Health = status.Health{Report: rep, Updated: time.Now().Add(-time.Minute)}
			status.Evaluate(s)
		}
	})
	page := func(p string, w int) {
		mygo.RunOnMain(func() {
			a.win.SetSize(w, 1100)
			old := a.page
			a.page = p
			a.pageChanged(old)
			a.win.Invalidate()
		})
		time.Sleep(1500 * time.Millisecond)
	}
	for _, w := range []int{1280, 900} {
		page(pageOverview, w)
		for range 3 { // the overview lays out with the previous frame's width
			save(a.win, "foreign-overview-"+strconv.Itoa(w))
			time.Sleep(300 * time.Millisecond)
		}
	}
	page(pageProxies, 1000)
	save(a.win, "foreign-proxies")
	mygo.RunOnMain(a.togglePanel)
	time.Sleep(1500 * time.Millisecond)
	var panel *mygo.Window
	mygo.RunOnMain(func() { panel = a.panel })
	if panel != nil {
		mygo.RunOnMain(func() { panel.CapturePage() }) // lay out and fit first
		time.Sleep(600 * time.Millisecond)
		save(panel, "foreign-panel")
	}
}

// Synthetic paths of the foreign process (no client known: the anonymous
// wording, the harder case).
const (
	shotForeignExe    = `C:\Tools\proxy\mihomo.exe`
	shotForeignParent = `C:\Tools\proxy\launcher.exe`
)

// shotOwnerRow is the controller-owner row as the check words it for
// owner, in the UI language.
func shotOwnerRow(owner *foreign.Owner) health.CheckReport {
	tag := i18n.Tag()
	ctx := health.WithLang(context.Background(), tag)
	env := health.WithOwner(nil, func(context.Context) foreign.Owner { return *owner })
	c := health.ControllerOwnerCheck{}
	return health.CheckReport{ID: c.ID(), Title: health.TitleOf(tag, c.ID()), Result: c.Run(ctx, env)}
}
