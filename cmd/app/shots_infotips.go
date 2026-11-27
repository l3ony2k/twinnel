package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// infoTipShots (DUET_SHOTS_FLOW=infotips) captures each page with one of
// its ⓘ open (shotTipOpen), from synthetic state: the fake snapshot with a
// few fields overridden in memory (shotSnap) and made-up helper lists.
// The helper is set, not connected: nothing is sent or written.
func (a *app) infoTipShots(save func(*mygo.Window, string)) {
	T := i18n.T
	connected := status.Helper{State: status.HelperConnected, Socket: "/tmp/duet-shots.sock",
		Status: &rpc.Status{HelperVersion: "0.2.0", PID: 4242, ActiveSubscription: "example",
			Mihomo: rpc.MihomoStatus{ConfigPath: "/Users/example/mihomo/conf/config.yaml", Version: "v1.19.33", Reachable: true}}}
	accepted := func(s *status.Snapshot) {
		if x := s.Tailscale.State.ExitNode; x != nil {
			s.AcceptedExitNode = x.Name
		}
	}
	healthy := func(s *status.Snapshot) {
		r := &health.Report{Platform: "darwin", Started: time.Now().Add(-2 * time.Minute), Duration: 1800 * time.Millisecond}
		add := func(id, title, summary string, st health.Status, by string) {
			r.Checks = append(r.Checks, health.CheckReport{ID: id, Title: title, CausedBy: by,
				Result: health.Result{Status: st, Summary: summary}})
		}
		add("exit", "Exit node", "exit-sg is in use while TUN is on", health.StatusAccepted, "")
		add("dns", "Tailnet DNS", "Answers through the exit node", health.StatusAccepted, "exit")
		for _, id := range []string{"tun", "route", "derp", "leak", "ctl"} {
			add(id, "Check "+id, "OK", health.StatusOK, "")
		}
		r.Overall = health.StatusOK
		s.Health = status.Health{Report: r, Updated: time.Now().Add(-2 * time.Minute)}
	}
	shot := func(name, page, section string, h status.Helper, height int, tip string, snap func(*status.Snapshot), prep func()) {
		apply := func() {
			a.mon.SetHelper(h)
			a.shotSnap = func(s *status.Snapshot) {
				s.Helper = h
				if snap != nil {
					snap(s)
				}
				s.Loaded = true
				status.Evaluate(s)
			}
			shotTipOpen = tip
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
		// A hidden window lays out only when captured: once to place the
		// popover by its anchor, then the shot.
		time.Sleep(400 * time.Millisecond)
		mygo.RunOnMain(func() { a.win.CapturePage() })
		time.Sleep(500 * time.Millisecond)
		save(a.win, name)
	}
	loaded := func() { a.hd.loaded = true }

	shot("overview-accepted", pageOverview, "", connected, 760, T.TipExitAccepted, accepted, nil)
	shot("overview-tun", pageOverview, "", connected, 760, T.InfoTun, nil, nil)
	shot("tailscale-path", pageTailscale, "", connected, 760, T.TipPeerPath, nil, nil)
	shot("tailscale-magicdns", pageTailscale, "", connected, 560, T.TipMagicDNS, nil, nil)
	shot("health-accepted", pageHealth, "", connected, 760, T.TipHealthAccepted, healthy, nil)

	hist := []rpc.HistoryEntry{
		{ID: "h3", Time: time.Now().Add(-1 * time.Hour), Hash: "sha256:3c1f0a9b7d2e4f6a8b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a", Size: 48_213, Kind: "applied", Reason: "Example override"},
		{ID: "h2", Time: time.Now().Add(-26 * time.Hour), Hash: "sha256:9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b", Size: 47_900, Kind: "applied", Reason: "Subscription update"},
		{ID: "h1", Time: time.Now().Add(-50 * time.Hour), Hash: "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0", Size: 45_002, Kind: "backup", BeforeDuet: true},
	}
	shot("history-rollback", pageHistory, "", connected, 760, T.TipHistRollback, nil, func() {
		loaded()
		a.hd.history = hist
		a.hd.hist.sel = "h2"
	})

	kernPrep := func() {
		loaded()
		a.hd.kernels = &rpc.KernelList{Active: "/Library/Application Support/duet/kernels/v1.19.34/mihomo", Current: "v1.19.34", Previous: "v1.19.33",
			Installed: []rpc.KernelVersion{{Version: "v1.19.34", Current: true, Path: "/Library/Application Support/duet/kernels/v1.19.34/mihomo"},
				{Version: "v1.19.33", Previous: true, Path: "/Library/Application Support/duet/kernels/v1.19.33/mihomo"}}}
		a.hd.kern.geo.check = &rpc.AccessoryCheck{Dir: "/Users/example/mihomo/conf", Files: []rpc.AccessoryFile{
			{Name: "GeoIP", File: "/Users/example/mihomo/conf/geoip.metadb", Installed: true, Size: 8_400_000,
				SHA256: "sha256:5d41402abc4b2a76b9719d911017c5925d41402abc4b2a76b9719d911017c592", Remote: "sha256:5d41402abc4b2a76b9719d911017c5925d41402abc4b2a76b9719d911017c592", Verifiable: true},
			{Name: "GeoSite", File: "/Users/example/mihomo/conf/geosite.dat", Installed: true, Size: 5_100_000,
				SHA256: "sha256:7d793037a0760186574b0282f2f435e77d793037a0760186574b0282f2f435e7"},
		}}
	}
	shot("kernel-state", pageKernel, "kernel", connected, 760, T.TipKernelState, nil, kernPrep)
	shot("kernel-geo", pageKernel, rpc.MethodGeoCheck, connected, 560, T.TipGeo, nil, kernPrep)

	shot("logs", pageLogs, "", connected, 560, T.TipLogs, nil, nil)
	shot("settings-login", pageSettings, "", connected, 760, T.TipOpenAtLogin, nil, nil)
	shot("settings-primary", pageSettings, "", connected, 760, T.TipPrimaryGroup, nil, nil)

	leave := leaveView{loaded: true, inv: &rpc.Inventory{Adopted: "plan-20261127-101500-5a5a5a5a", Items: []rpc.InventoryItem{
		{Kind: rpc.InvHelperService, Path: "/Library/LaunchDaemons/com.l3ony2k.duet.helper.plist", Label: "com.l3ony2k.duet.helper"},
		{Kind: rpc.InvStateDir, Path: "/Library/Application Support/duet", Size: 61_400_000},
	}}}
	shot("leave-removeall", pageLeave, "actions", connected, 760, T.TipLeaveRemoveAll, nil, func() { a.hd.leave = leave })
	shot("leave-undo", pageLeave, "actions", connected, 560, T.TipLeaveUndo, nil, func() { a.hd.leave = leave })

	notInstalled := status.Helper{State: status.HelperNotInstalled}
	shot("install-plan", pageInstall, "", notInstalled, 760, T.TipInstallPlan, nil, func() {
		a.hd.onb = onboardingView{loaded: true, planErr: "duet: example plan error"}
	})

	// 了解更多 on 登录时打开: 关于 opens at 退出 duet 会断网吗？, open.
	shot("about-faq", pageAbout, "", connected, 760, "", nil, func() {
		if a.page == pageAbout && a.about.reveal == "" && !a.about.faqOpen[T.FAQQuitQ] {
			a.openAboutFAQ(T.FAQQuitQ)
		}
	})
	mygo.RunOnMain(func() { shotTipOpen, a.shotSnap = "", nil })
}
