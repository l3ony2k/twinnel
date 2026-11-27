package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/mihomo"
)

// toolbarForce, set only by the toolbar shot flow, draws every toolbar
// capsule (and every unchosen toolbar segment) as hovered or pressed:
// "hover" or "pressed". Shots cannot move the pointer, and this is the
// same code path a real hover takes. Main thread only.
var toolbarForce string

// toolbarShots saves each page that has toolbar actions with its
// capsules idle, hovered and pressed (TWINNEL_SHOTS_FLOW=toolbar).
func (a *app) toolbarShots(save func(*mygo.Window, string)) {
	for _, p := range []string{pageProxies, pageTailscale, pageHealth, pageLogs} {
		for _, state := range []string{"", "hover", "pressed"} {
			mygo.RunOnMain(func() {
				old := a.page
				a.page = p
				a.pageChanged(old)
				toolbarForce = state
				a.win.Invalidate()
			})
			time.Sleep(900 * time.Millisecond)
			name := "toolbar-" + p
			if state != "" {
				name += "-" + state
			}
			save(a.win, name)
		}
	}
	mygo.RunOnMain(func() { toolbarForce = "" })

	// 日志 → 后台助手 with synthetic lines (shots never fetch the log).
	mygo.RunOnMain(func() {
		t0 := time.Date(2026, 1, 2, 9, 30, 0, 0, time.Local)
		a.logs.helper.entries = mihomo.ParseLogLines([]string{
			"time=" + t0.Format(time.RFC3339Nano) + ` level=INFO msg="helper started" version=0.1.0 pid=4242`,
			"time=" + t0.Add(time.Minute).Format(time.RFC3339Nano) + ` level=INFO msg=rpc method=compose.apply peer.uid=501 peer.user=alice peer.pid=812 dur=420ms`,
			"time=" + t0.Add(2*time.Minute).Format(time.RFC3339Nano) + ` level=WARN msg="subscription fetch" sub=main err="502 Bad Gateway"`,
		})
		a.logs.source = logsHelper
		a.win.Invalidate()
	})
	time.Sleep(900 * time.Millisecond)
	save(a.win, "logs-helper")
	mygo.RunOnMain(func() { a.logs.source, a.logs.helper.entries = logsMihomo, nil })
}
