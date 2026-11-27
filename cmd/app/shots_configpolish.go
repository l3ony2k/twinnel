package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// configPolishShots (TWINNEL_SHOTS_FLOW=configpolish) walks the 订阅与覆写
// page's states against a scratch dev helper (TWINNEL_SOCKET, writes on)
// whose state starts empty: the empty page, a subscription with two user
// overrides, the managed rows opened, unapplied changes and their
// preview, a failed apply, and the drift after a rollback.
// TWINNEL_POLISH_DATA is the folder with synthetic subscription.yaml.
// Confirm dialogs are never shown (they take the focus).
func (a *app) configPolishShots(save func(*mygo.Window, string)) {
	on := func(fn func()) { mygo.RunOnMain(func() { fn(); a.invalidate() }) }
	wait := func(d time.Duration) {
		deadline := time.Now().Add(30 * time.Second)
		time.Sleep(d)
		for time.Now().Before(deadline) {
			busy := true
			mygo.RunOnMain(func() { busy = a.hd.busy != "" || a.hd.loading || a.hd.unappliedCheck })
			time.Sleep(200 * time.Millisecond)
			if !busy {
				break
			}
		}
		time.Sleep(600 * time.Millisecond)
	}
	call := func(method string, p any) {
		var hc *status.HelperConn
		mygo.RunOnMain(func() { hc = a.hc })
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		if err := hc.Call(ctx, method, p, nil); err != nil {
			log.Printf("shot %s: %v", method, err)
		}
		cancel()
		on(func() { a.loadHelperData(); a.checkUnapplied() })
		wait(800 * time.Millisecond)
	}
	scroll := func(y float32) {
		on(func() {
			if st := a.pageScroll[a.page]; st != nil {
				st.Y = y
			}
		})
		time.Sleep(500 * time.Millisecond)
	}
	layer := func(id string) (l rpc.Layer) {
		mygo.RunOnMain(func() {
			if i := indexLayer(a.hd.layers, id); i >= 0 {
				l = a.hd.layers[i]
			}
		})
		return l
	}
	closeLayer := func() { on(func() { a.hd.cfg.sel, a.hd.cfg.isNew, a.hd.cfg.ed = "", false, nil }) }

	on(func() {
		old := a.page
		a.page = pageConfig
		a.pageChanged(old)
	})
	wait(1500 * time.Millisecond)
	save(a.win, "p01-empty")

	// A subscription and two user overrides, applied.
	sub, _ := os.ReadFile(filepath.Join(os.Getenv("TWINNEL_POLISH_DATA"), "subscription.yaml"))
	mins := 1440
	call(rpc.MethodSubPut, rpc.SubPutParams{ID: "main", Name: "Example Cloud", Content: string(sub), IntervalMinutes: &mins})
	call(rpc.MethodSubActivate, rpc.IDParams{ID: "main"})
	call(rpc.MethodLayerPut, rpc.LayerPutParams{ID: "direct-lan", Name: "局域网直连", Content: "# synthetic\n+rules:\n  - DOMAIN-SUFFIX,lan.example,DIRECT\n  - IP-CIDR,192.0.2.0/24,DIRECT,no-resolve\n"})
	call(rpc.MethodLayerPut, rpc.LayerPutParams{ID: "dns", Name: "DNS", Content: "# synthetic\ndns:\n  nameserver:\n    - https://dns.example/dns-query\n"})
	call(rpc.MethodComposeApply, rpc.ApplyParams{Reason: "seed"})
	save(a.win, "p02-composed")

	// The managed rows, opened.
	on(func() { a.selectLayer(layer(coexistLayerID)) })
	wait(time.Second)
	save(a.win, "p03-coexist-open")
	on(func() { a.hd.cfg.showGen = true })
	wait(time.Second)
	save(a.win, "p03b-coexist-generated")
	on(func() { a.hd.cfg.showGen = false })
	on(func() { a.selectLayer(layer("leak-guard")) })
	wait(time.Second)
	save(a.win, "p04-guard-empty")
	groups := []string{"PROXY"}
	call(rpc.MethodSettingsUpdate, rpc.SettingsUpdate{LeakGuardGroups: &groups})
	on(func() { a.selectLayer(layer("leak-guard")) })
	wait(time.Second)
	save(a.win, "p05-guard-on")
	closeLayer()

	// Unapplied changes, then their preview.
	call(rpc.MethodLayerPut, rpc.LayerPutParams{ID: "direct-lan", Content: "# synthetic\n+rules:\n  - DOMAIN-SUFFIX,lan.example,DIRECT\n  - DOMAIN-SUFFIX,nas.example,DIRECT\n  - IP-CIDR,192.0.2.0/24,DIRECT,no-resolve\n"})
	wait(time.Second)
	save(a.win, "p06-pending")
	on(func() { a.runPreview(a.mon.Snapshot()) })
	wait(2 * time.Second)
	save(a.win, "p07-preview")
	scroll(100000)
	save(a.win, "p07b-preview-end")
	scroll(0)

	// A failed apply (mihomo -t refuses).
	call(rpc.MethodLayerPut, rpc.LayerPutParams{ID: "broken", Name: "坏的覆写", Content: "# synthetic\n+rules:\n  - DOMAIN,twinnel-test-invalid.example,DIRECT\n"})
	on(a.runApply)
	wait(2 * time.Second)
	save(a.win, "p08-apply-failed")
	scroll(100000)
	save(a.win, "p08b-apply-failed-end")
	scroll(0)
	call(rpc.MethodLayerDelete, rpc.IDParams{ID: "broken"})
	on(a.runApply)
	wait(2 * time.Second)
	save(a.win, "p09-applied")

	// Drift: roll back to the first entry.
	var hist []rpc.HistoryEntry
	mygo.RunOnMain(func() { hist = a.hd.history })
	var target *rpc.HistoryEntry
	for i := range hist {
		if hist[i].Kind != "backup" && hist[i].Layers != nil {
			target = &hist[i]
		}
	}
	if target != nil {
		on(func() { a.historyRollbackNow(*target) })
		wait(2 * time.Second)
		on(func() {
			old := a.page
			a.page = pageConfig
			a.pageChanged(old)
		})
		wait(time.Second)
		save(a.win, "p10-drift")
	}
}
