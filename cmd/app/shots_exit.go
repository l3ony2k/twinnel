package main

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/health"
)

// exitShots (TWINNEL_SHOTS_FLOW=exit) shows 总览's 出口 tile: as it starts
// (未测 with 测速 and 检测出口 IP against `fakemihomo -no-history`; mihomo's
// history with its age against the plain fake), while the delay test and
// then the egress test run, with both results, after a failed egress run,
// and 健康检查's 出口 IP 实测 row opened with the addresses. The egress test
// is the fixture's probe (synthetic addresses), slowed so its progress
// shows, and said to have gone through the shown exit.
func (a *app) exitShots(save func(*mygo.Window, string)) {
	step := func(name string, wait time.Duration, f func()) {
		mygo.RunOnMain(func() {
			f()
			a.win.Invalidate()
		})
		time.Sleep(wait)
		save(a.win, name)
	}
	first := "exit-untested"
	mygo.RunOnMain(func() {
		old := a.page
		a.page = pageOverview
		a.pageChanged(old)
		a.win.Invalidate()
	})
	time.Sleep(1200 * time.Millisecond)
	if p := a.mon.Snapshot().Mihomo.Primary; p != nil && p.Tested {
		first = "exit-history"
	}
	save(a.win, first)

	probe := a.egressProbe
	a.egressProbe = func(ctx context.Context) *health.CheckReport {
		time.Sleep(1500 * time.Millisecond)
		cr := probe(ctx)
		if cr != nil && cr.Egress != nil {
			if p := a.mon.Snapshot().Mihomo.Primary; p != nil {
				cr.Egress.Node = p.Node
			}
		}
		return cr
	}
	step("exit-testing", 150*time.Millisecond, func() {
		if !a.mon.TestExit() {
			a.testNode(a.mon.Snapshot().Mihomo.Primary.Node)
		}
		a.testEgress()
	})
	step("exit-egress-running", 600*time.Millisecond, func() {})
	time.Sleep(1200 * time.Millisecond)
	step("exit-result", 500*time.Millisecond, func() {})
	step("exit-failed", 600*time.Millisecond, func() {
		err := errors.New("dial unix /tmp/twinnel.sock: connect: connection refused")
		a.egress.err, a.egress.why = err.Error(), helperError(err)
	})
	step("exit-health-ips", time.Second, func() {
		a.egress.err, a.egress.why = "", ""
		old := a.page
		a.page = pageHealth
		a.pageChanged(old)
		a.healthOpen[health.LeakEgress{}.ID()] = true
		if st := a.pageScroll[pageHealth]; st != nil {
			st.Y = 10000 // the row sits near the end of the list
		}
	})
}
