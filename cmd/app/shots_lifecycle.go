package main

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// lifecycleShots (DUET_SHOTS_FLOW=lifecycle) captures batch A's screens
// from synthetic state: the takeover plan (现在 | 接管后, 订阅原文, the
// verdict and the 接管 row) and result (下一步) from
// DUET_TAKEOVER_FIXTURE (default testdata/takeover-a), 停止使用 duet with
// a made-up inventory, the helper's 未运行 card, the install preview's
// failed state, the 内核 page with an adopted service, 关于 and 设置.
// Nothing is sent to a helper and nothing is written.
func (a *app) lifecycleShots(save func(*mygo.Window, string)) {
	fix := os.Getenv("DUET_TAKEOVER_FIXTURE")
	if fix == "" {
		fix = filepath.Join("cmd", "app", "testdata", "takeover-a")
	}
	connected := status.Helper{State: status.HelperConnected, Socket: "/var/run/duet/helper.sock",
		Status: &rpc.Status{HelperVersion: "0.2.0", PID: 4242, Mihomo: rpc.MihomoStatus{
			ConfigPath: "/Users/example/mihomo/conf/config.yaml", Version: "v1.19.33", Reachable: true}}}
	shot := func(name, page, section string, h status.Helper, height int, prep func()) {
		apply := func() {
			a.mon.SetHelper(h)
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
		time.Sleep(400 * time.Millisecond)
		save(a.win, name)
	}

	// Takeover.
	var fixErr error
	mygo.RunOnMain(func() { fixErr = a.loadTakeoverFixture(fix) })
	if fixErr != nil {
		log.Printf("takeover fixture %s: %v", fix, fixErr)
	}
	adopt := a.hd.take.adopt
	noResult := func() {
		a.hd.take.adopt = nil
		if a.hd.take.plan != nil {
			a.hd.take.plan.State = rpc.PlanReady
		}
		a.hd.take.plans = []rpc.TakeoverPlan{*a.hd.take.plan}
	}
	shot("takeover-plan", pageTakeover, "plan", connected, 1300, func() {
		noResult()
		a.hd.take.candOpen, a.hd.take.othersOpen = true, true
	})
	shot("takeover-verdict", pageTakeover, "verdict", connected, 1500, func() {
		noResult()
		a.hd.take.keysOpen = nil
	})
	shot("takeover-empty", pageTakeover, "plan", connected, 900, func() {
		a.hd.take.plan, a.hd.take.plans, a.hd.take.adopt = nil, nil, nil
	})
	mygo.RunOnMain(func() { a.loadTakeoverFixture(fix) })
	shot("takeover-result", pageTakeover, "result", connected, 1200, func() {
		a.hd.take.adopt = adopt
		if a.hd.take.plan != nil {
			a.hd.take.plan.State = rpc.PlanAdopted
			a.hd.take.plans = []rpc.TakeoverPlan{*a.hd.take.plan}
		}
	})

	// 停止使用 duet.
	home := "/Users/example"
	leave := leaveView{loaded: true, inv: &rpc.Inventory{Adopted: "plan-20261127-101500-5a5a5a5a", Items: []rpc.InventoryItem{
		{Kind: rpc.InvHelperBinary, Path: "/Library/PrivilegedHelperTools/com.l3ony2k.duet.helper", Size: 14_200_000},
		{Kind: rpc.InvHelperService, Path: "/Library/LaunchDaemons/com.l3ony2k.duet.helper.plist", Label: "com.l3ony2k.duet.helper"},
		{Kind: rpc.InvHelperSocket, Path: "/var/run/duet/helper.sock"},
		{Kind: rpc.InvStateDir, Path: "/Library/Application Support/duet", Size: 61_400_000},
		{Kind: rpc.InvSubscriptions, Path: "/Library/Application Support/duet/subscriptions", Size: 48_213, Count: 1},
		{Kind: rpc.InvLayers, Path: "/Library/Application Support/duet/layers", Size: 3_100, Count: 2},
		{Kind: rpc.InvHistory, Path: "/Library/Application Support/duet/history", Size: 912_000, Count: 14},
		{Kind: rpc.InvTakeoverBackups, Path: "/Library/Application Support/duet/takeover", Size: 220_000, Count: 1},
		{Kind: rpc.InvKernels, Path: "/Library/Application Support/duet/kernels", Size: 59_800_000},
		{Kind: rpc.InvLogs, Path: "/Library/Application Support/duet/logs", Size: 380_000},
		{Kind: rpc.InvSecrets, Names: []string{"controller-secret", "subscription-url/adopted"}},
		{Kind: rpc.InvAdoptedService, Path: "/Library/LaunchDaemons/com.example.mihomo.plist", Label: "com.example.mihomo"},
	}}, app: []rpc.InventoryItem{
		{Kind: invApp, Path: "/Applications/duet.app", Size: 31_000_000},
		{Kind: invAppPrefs, Path: filepath.Join(home, "Library", "Application Support", "duet", "app.json"), Size: 412},
		{Kind: invAppBackups, Path: filepath.Join(home, "Library", "Application Support", "duet", "backups"), Size: 62_000_000, Count: 2},
		{Kind: invLoginItem},
	}}
	shot("leave", pageLeave, "", connected, 1500, func() { a.hd.leave = leave })
	undone := leave
	undone.undo = &rpc.TakeoverUndoResult{OK: true, PlanID: leave.inv.Adopted, Steps: []rpc.TakeoverUndoStep{
		{Code: rpc.UndoRestoreConfig, Path: "/Users/example/mihomo/conf/config.yaml", Status: "ok"},
		{Code: rpc.UndoReload, Status: "ok"},
		{Code: rpc.UndoRemoveSub, Status: "ok"},
		{Code: rpc.UndoRemoveLayers, Count: 2, Status: "ok"},
		{Code: rpc.UndoKeepService, Path: "com.example.mihomo", Status: "ok"},
		{Code: rpc.UndoKeepData, Status: "ok"},
	}}
	undone.inv = &rpc.Inventory{Items: leave.inv.Items}
	shot("leave-undone", pageLeave, "actions", connected, 1000, func() { a.hd.leave = undone })
	shot("settings-leave-entry", pageSettings, "", connected, 1700, func() {
		a.hm = helperMgmtView{check: &upgradeCheck{Action: "none"}, checkPID: 4242}
	})

	// Helper states and the install preview.
	notRunning := status.Helper{State: status.HelperNotRunning, Socket: "/var/run/duet/helper.sock", Err: "dial unix /var/run/duet/helper.sock: connect: connection refused"}
	shot("helper-not-running", pageConfig, "", notRunning, 700, nil)
	shot("install-preview-failed", pageInstall, "", status.Helper{State: status.HelperNotInstalled}, 1000, func() {
		a.hd.onb = onboardingView{loaded: true, planErr: "duet: cannot find duet-helper next to the command line tool"}
	})
	shot("install-preview-timeout", pageInstall, "", status.Helper{State: status.HelperNotInstalled}, 1000, func() {
		a.hd.onb = onboardingView{loaded: true, timeout: true, planErr: "signal: killed"}
	})

	// 内核: an adopted service, a managed kernel waiting for a restart.
	kernPrep := func() {
		a.hm = helperMgmtView{check: &upgradeCheck{Action: "none"}, checkPID: 4242,
			info: &rpc.HelperInfo{Mihomo: rpc.HelperMihomoService{Label: "com.example.mihomo", Adopted: true, Installed: true}}}
		a.hd.kernels = &rpc.KernelList{Active: "/Library/Application Support/duet/kernels/v1.19.34/mihomo", Current: "v1.19.34", Previous: "v1.19.33",
			Installed: []rpc.KernelVersion{{Version: "v1.19.34", Current: true, Path: "/Library/Application Support/duet/kernels/v1.19.34/mihomo"},
				{Version: "v1.19.33", Previous: true, Path: "/Library/Application Support/duet/kernels/v1.19.33/mihomo"}}}
		a.hd.kern.svc = serviceView{status: map[string]rpc.ServiceStatus{
			"mihomo": {Name: "mihomo", Label: "com.example.mihomo", Installed: true, Loaded: true, Running: true, PID: 812,
				Program: "/Library/Application Support/duet/kernels/v1.19.34/mihomo", Definition: "/Library/LaunchDaemons/com.example.mihomo.plist"},
			"tailscaled": {Name: "tailscaled", Label: "homebrew.mxcl.tailscale", Installed: true, Loaded: true, Running: true, PID: 640,
				Definition: "/Library/LaunchDaemons/homebrew.mxcl.tailscale.plist"},
		}}
		a.hd.kern.checkErr = "kernel.check: GET https://api.github.com/repos/MetaCubeX/mihomo/releases: 403 rate limited"
	}
	shot("kernel", pageKernel, "kernel", connected, 1100, kernPrep)
	shot("kernel-services", pageKernel, "services", connected, 800, kernPrep)

	// 关于: the FAQ's way out and the two file rows.
	shot("about", pageAbout, "", connected, 1900, func() {
		if a.about.faqOpen == nil {
			a.about.faqOpen = map[string]bool{}
		}
		a.about.faqOpen[i18n.T.FAQLeaveQ] = true
	})
}
