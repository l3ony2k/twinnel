package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// dnsWatchShots (TWINNEL_SHOTS_FLOW=dnswatch) captures the DNS watchdog's
// state: mihomo down and the system DNS temporarily back to the earlier
// settings, on the overview (banner with 重启 mihomo / 查看日志 / 保持原设置)
// and in the flyout. The state is the live (fake) snapshot with fields
// overridden in memory (shotSnap); nothing is written.
func (a *app) dnsWatchShots(save shotSaver) {
	restored := func(s *status.Snapshot) {
		s.Helper = status.Helper{State: status.HelperConnected, Status: &rpc.Status{HelperVersion: "0.1.0",
			DNSWatch: &rpc.DNSWatch{Phase: rpc.DNSWatchRestored, Cause: rpc.DNSCauseStopped,
				Since: time.Now().Add(-3 * time.Minute), Services: []string{"Wi-Fi"}}}}
		s.Mihomo = status.Mihomo{Err: "dial tcp 127.0.0.1:19894: connect: connection refused"}
		s.WritesAllowed = true // show the helper actions; nothing is clicked
		s.Loaded = true
		status.Evaluate(s)
	}
	mygo.RunOnMain(func() {
		a.shotSnap = restored
		a.win.SetSize(a.winWidth(), 900)
		old := a.page
		a.page = pageOverview
		a.pageChanged(old)
		a.win.Invalidate()
	})
	time.Sleep(1500 * time.Millisecond)
	save(a.win, "dnswatch-overview")
	mygo.RunOnMain(a.togglePanel)
	time.Sleep(1500 * time.Millisecond)
	var panel *mygo.Window
	mygo.RunOnMain(func() { panel = a.panel })
	if panel != nil {
		mygo.RunOnMain(func() { panel.CapturePage() }) // lay out and fit first
		time.Sleep(600 * time.Millisecond)
		save(panel, "dnswatch-panel")
	}
}
