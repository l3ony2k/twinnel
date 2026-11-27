package main

import (
	"context"
	"time"

	"github.com/egoist/mygo"
)

// delayShots (TWINNEL_SHOTS_FLOW=delay) shows 全部测速 on the proxies page:
// before, while the batch runs (spinner and progress) and after. Against
// the fake controller only; delay tests are reads either way.
func (a *app) delayShots(save func(*mygo.Window, string)) {
	mygo.RunOnMain(func() {
		old := a.page
		a.page = pageProxies
		a.pageChanged(old)
		for _, g := range []string{"PROXY", "OpenAI", "Apple"} {
			a.groupsOpen[g] = true
		}
		a.win.Invalidate()
	})
	time.Sleep(700 * time.Millisecond)
	save(a.win, "delay-before")
	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = a.mon.TestAll(ctx)
		close(done)
	}()
	time.Sleep(450 * time.Millisecond) // past busyDelay, first results in
	save(a.win, "delay-testing")
	<-done
	time.Sleep(700 * time.Millisecond)
	save(a.win, "delay-after")
}
