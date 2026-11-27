package main

import (
	"errors"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/health"
)

// egressShots (DUET_SHOTS_FLOW=egress) shows 健康检查 → 出口 IP 实测:
// untested (检测出口 and 定期检测 on the row), while the test runs, with
// its result and time (closed and opened), a failed run and 定期检测 on. Shots run the fixture's probe, never the helper's real test.
func (a *app) egressShots(save func(*mygo.Window, string)) {
	id := health.LeakEgress{}.ID()
	step := func(name string, wait time.Duration, f func()) {
		mygo.RunOnMain(func() {
			f()
			if st := a.pageScroll[pageHealth]; st != nil {
				st.Y = 10000 // the row sits near the end of the list
			}
			a.win.Invalidate()
		})
		time.Sleep(wait)
		save(a.win, name)
	}
	step("egress-untested", time.Second, func() {
		old := a.page
		a.page = pageHealth
		a.pageChanged(old)
		a.healthOpen[id] = false
	})
	step("egress-testing", 450*time.Millisecond, a.testEgress)
	time.Sleep(time.Second)
	step("egress-result", 700*time.Millisecond, func() {})
	step("egress-result-open", 700*time.Millisecond, func() { a.healthOpen[id] = true })
	step("egress-failed", 700*time.Millisecond, func() {
		a.healthOpen[id] = false
		err := errors.New("dial unix /tmp/duet.sock: connect: connection refused")
		a.egress.err, a.egress.why = err.Error(), helperError(err)
	})
	step("egress-periodic", 700*time.Millisecond, func() {
		a.egress.err, a.egress.why = "", ""
		if a.hd.settings != nil {
			a.hd.settings.HealthEgress = true // shown only: shots never write the setting
		}
	})
}
