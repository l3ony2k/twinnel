package main

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// takeoverExplainShots (DUET_SHOTS_FLOW=takeoverexplain) captures the
// takeover explanation from synthetic state: the 接管 page before any plan
// (intro, the detected setup, 生成接管计划 with its note), the same with
// nothing detected, a plan's cards with their ⓘ, 关于 at the top (mihomo
// 配置 row) and scrolled to 接管是什么 as 了解接管 does, and 内核 before
// takeover. The helper is set, not connected: nothing is sent or written.
func (a *app) takeoverExplainShots(save func(*mygo.Window, string)) {
	helperDefault := "/Library/Application Support/duet/mihomo/config.yaml"
	connected := status.Helper{State: status.HelperConnected, Socket: "/tmp/duet-shots.sock",
		Status: &rpc.Status{HelperVersion: "0.2.0", PID: 4242, Mihomo: rpc.MihomoStatus{
			ConfigPath: helperDefault, Version: "v1.19.34", Reachable: true}}}
	detected := liveSetup{loaded: true, Label: "com.example.mihomo",
		Plist:  "/Library/LaunchDaemons/com.example.mihomo.plist",
		Binary: "/Users/example/mihomo/bin/mihomo", Config: "/Users/example/mihomo/conf/config.yaml"}
	shot := func(name, page, section string, height int, prep func()) {
		apply := func() {
			a.mon.SetHelper(connected)
			if prep != nil {
				prep()
			}
			a.win.Invalidate()
		}
		mygo.RunOnMain(func() {
			a.win.SetSize(a.winWidth(), height)
			old := a.page
			a.page = page
			a.pageChanged(old)
			a.shotSection = section
			apply()
		})
		time.Sleep(900 * time.Millisecond)
		mygo.RunOnMain(apply) // the helper connection may have set its own state meanwhile
		time.Sleep(700 * time.Millisecond)
		save(a.win, name)
	}
	empty := func() { a.hd.take.plan, a.hd.take.plans, a.hd.take.adopt = nil, nil, nil }

	shot("takeover-intro", pageTakeover, "", 820, func() { empty(); a.live = detected })
	shot("takeover-notdetected", pageTakeover, "", 820, func() { empty(); a.live = liveSetup{loaded: true} })

	fix := os.Getenv("DUET_TAKEOVER_FIXTURE")
	if fix == "" {
		fix = filepath.Join("cmd", "app", "testdata", "takeover-a")
	}
	var fixErr error
	mygo.RunOnMain(func() { fixErr = a.loadTakeoverFixture(fix) })
	if fixErr != nil {
		log.Printf("takeover fixture %s: %v", fix, fixErr)
	} else {
		ready := func() {
			a.live = detected
			a.hd.take.adopt = nil
			if a.hd.take.plan != nil {
				a.hd.take.plan.State = rpc.PlanReady
				a.hd.take.plans = []rpc.TakeoverPlan{*a.hd.take.plan}
			}
		}
		shot("takeover-plan-tips", pageTakeover, "plan", 1300, ready)
		shot("takeover-verdict-tips", pageTakeover, "verdict", 1500, ready)
	}

	mygo.RunOnMain(func() { a.hd.take.fixture = false })
	shot("about-top", pageAbout, "", 900, func() { a.live = detected })
	mygo.RunOnMain(func() { a.page = pageOverview; a.pageChanged(pageAbout) })
	time.Sleep(300 * time.Millisecond)
	// 了解接管: 关于 opens scrolled to 接管是什么.
	shot("about-takeover", pageAbout, "", 900, func() {
		a.live = detected
		if a.page != pageAbout || a.about.reveal == "" {
			a.about.reveal = aboutTakeoverAnchor
		}
	})
	shot("kernel-own", pageKernel, "", 900, func() {
		a.live = detected
		a.hd.kernels = &rpc.KernelList{Active: "/Library/Application Support/duet/kernels/v1.19.34/mihomo", Current: "v1.19.34",
			Installed: []rpc.KernelVersion{{Version: "v1.19.34", Path: "/Library/Application Support/duet/kernels/v1.19.34/mihomo", Current: true}}}
	})
}
