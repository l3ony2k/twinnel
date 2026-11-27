package main

import (
	"bytes"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/bundle"
	"github.com/l3ony2k/twinnel/core/store"
)

// importShots (TWINNEL_SHOTS_FLOW=importcfg) opens 导入配置 on a synthetic
// export against the scratch helper: the dry run only, nothing is stored.
// One of its layers takes the id of the first stored layer, so the sheet
// shows a conflict row.
func (a *app) importShots(save func(*mygo.Window, string)) {
	on := func(fn func()) { mygo.RunOnMain(func() { fn(); a.invalidate() }) }
	wait := func(d time.Duration) {
		deadline := time.Now().Add(30 * time.Second)
		time.Sleep(d)
		for time.Now().Before(deadline) {
			busy := true
			mygo.RunOnMain(func() { busy = a.hd.busy != "" || a.hd.loading })
			time.Sleep(200 * time.Millisecond)
			if !busy {
				break
			}
		}
		time.Sleep(600 * time.Millisecond)
	}
	on(func() {
		old := a.page
		a.page = pageConfig
		a.pageChanged(old)
	})
	wait(time.Second)

	taken := "my-rules"
	mygo.RunOnMain(func() {
		for _, l := range a.hd.layers {
			if !l.Managed && l.Stage != stageBase {
				taken = l.ID
				break
			}
		}
	})
	b := &bundle.Bundle{
		Config: "mode: rule\n",
		Layers: []bundle.Layer{
			{ID: "adopted-base", Name: "Takeover base", Stage: store.StageBase, Format: store.FormatMerge, Enabled: true, Content: "mode: rule\n"},
			{ID: taken, Name: "Office rules", Stage: store.StageUser, Format: store.FormatMerge, Enabled: true,
				Content: "+rules:\n  - DOMAIN-SUFFIX,office.example,DIRECT\n"},
			{ID: "streaming", Name: "Streaming", Stage: store.StageUser, Format: store.FormatVerge, Enabled: false,
				Content: "prepend-rules:\n  - DOMAIN-SUFFIX,video.example,PROXY\n"},
		},
		Subscriptions: []bundle.Subscription{
			{ID: "home-provider", Name: "Home provider", Active: true, IntervalMinutes: 1440, URL: "https://sub.example.invalid/s?token=synthetic"},
		},
	}
	var buf bytes.Buffer
	bundle.Write(&buf, b, time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC), "0.9.0")
	on(func() { a.planImport("twinnel-config-20261128.zip", buf.Bytes()) })
	wait(1500 * time.Millisecond)
	save(a.win, "imp01-sheet")

	// Keep both for the conflict.
	on(func() {
		for _, ch := range a.hd.cfg.imp.choice {
			*ch = 2
		}
	})
	time.Sleep(600 * time.Millisecond)
	save(a.win, "imp02-keep-both")
	on(a.closeImport)
}
