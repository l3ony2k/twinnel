package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/health"
)

// egressShots (DUET_SHOTS_FLOW=egress) shows 健康检查 → 出口 IP 实测: off,
// opened (检测出口, 定期检测), while the test runs and with its result
// and time. Shots run the fixture's probe, never the helper's real test.
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
	step("egress-off", time.Second, func() {
		old := a.page
		a.page = pageHealth
		a.pageChanged(old)
	})
	step("egress-off-open", 700*time.Millisecond, func() { a.healthOpen[id] = true })
	step("egress-testing", 450*time.Millisecond, a.testEgress)
	time.Sleep(time.Second)
	step("egress-result", 700*time.Millisecond, func() {})
	step("egress-result-closed", 700*time.Millisecond, func() { a.healthOpen[id] = false })
}
