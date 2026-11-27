package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/rpc"
)

// overviewShots (TWINNEL_SHOTS_FLOW=overview) captures 总览 at the widths it
// is laid out for: 900 (two tiles a row, cards stacked or paired by
// language), 1280 and 1600 (four tiles, cards paired). When the helper
// has no events yet, 最近活动 shows synthetic ones (memory only).
func (a *app) overviewShots(save func(*mygo.Window, string)) {
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		n := 0
		mygo.RunOnMain(func() { a.backfillEvents(); n = len(a.events.evs) })
		if n > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	mygo.RunOnMain(func() {
		if len(a.events.evs) == 0 {
			a.recordEvents(shotEvents(time.Now()))
		}
	})
	for _, w := range []int{900, 1280, 1600} {
		mygo.RunOnMain(func() {
			a.win.SetSize(w, 1500)
			old := a.page
			a.page = pageOverview
			a.pageChanged(old)
			a.win.Invalidate()
		})
		time.Sleep(1500 * time.Millisecond)
		// A headless shot draws one frame per save, and the overview
		// lays out with the width the frame before measured: the first
		// frames after a resize still see the old width.
		for range 3 {
			save(a.win, fmt.Sprintf("overview-%d", w))
			time.Sleep(300 * time.Millisecond)
		}
	}
}

// shotEvents are synthetic twinnel events: an apply, a check that changed,
// a subscription refresh.
func shotEvents(now time.Time) []rpc.Event {
	data := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	return []rpc.Event{
		{Seq: 1, Time: now.Add(-3 * time.Hour), Type: rpc.EventSubUpdated, Data: data(map[string]any{"id": "main", "changed": true})},
		{Seq: 2, Time: now.Add(-3*time.Hour + time.Minute), Type: rpc.EventComposeApplied, Data: data(rpc.ApplyResult{Reason: "subscription", Written: true, Reloaded: true})},
		{Seq: 3, Time: now.Add(-20 * time.Minute), Type: rpc.EventTailnetChanged},
	}
}
