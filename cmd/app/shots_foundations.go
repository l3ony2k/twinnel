package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// rowForce, set only by the foundations shot flow, draws every rowFill
// row as hovered, so a capture shows the fill's extent.
var rowForce string

// foundationsShots (DUET_SHOTS_FLOW=foundations) checks the shared layout
// rules on every page: list rows' fills edge to edge, headings on the
// column's edge, technical values in mono, controls in kv rows at their
// natural width. With DUET_SHOTS_WRITE=1 against a scratch helper it
// first seeds a synthetic subscription (DUET_POLISH_DATA) and two
// overrides, so 订阅与覆写 has rows.
func (a *app) foundationsShots(save func(*mygo.Window, string)) {
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
		time.Sleep(500 * time.Millisecond)
	}
	if os.Getenv("DUET_SHOTS_WRITE") == "1" {
		call := func(method string, p any) {
			var hc *status.HelperConn
			mygo.RunOnMain(func() { hc = a.hc })
			if hc == nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			if err := hc.Call(ctx, method, p, nil); err != nil {
				log.Printf("shot %s: %v", method, err)
			}
			cancel()
		}
		sub, _ := os.ReadFile(filepath.Join(os.Getenv("DUET_POLISH_DATA"), "subscription.yaml"))
		mins := 1440
		call(rpc.MethodSubPut, rpc.SubPutParams{ID: "main", Name: "Example Cloud", Content: string(sub), IntervalMinutes: &mins})
		call(rpc.MethodSubActivate, rpc.IDParams{ID: "main"})
		call(rpc.MethodLayerPut, rpc.LayerPutParams{ID: "direct-lan", Name: "局域网直连", Content: "# synthetic\n+rules:\n  - DOMAIN-SUFFIX,lan.example,DIRECT\n  - IP-CIDR,192.0.2.0/24,DIRECT,no-resolve\n"})
		call(rpc.MethodLayerPut, rpc.LayerPutParams{ID: "dns", Name: "DNS", Content: "# synthetic\ndns:\n  nameserver:\n    - https://dns.example/dns-query\n"})
		call(rpc.MethodComposeApply, rpc.ApplyParams{Reason: "seed"})
		on(func() { a.loadHelperData() })
		wait(time.Second)
	}
	mygo.RunOnMain(func() { a.win.SetSize(a.winWidth(), 1300) })
	for _, p := range []string{pageOverview, pageProxies, pageTailscale, pageHealth, pageConfig, pageHistory,
		pageKernel, pageTakeover, pageSettings, pageAbout} {
		on(func() {
			old := a.page
			a.page = p
			a.pageChanged(old)
		})
		wait(900 * time.Millisecond)
		save(a.win, "f-"+p)
		if p == pageConfig {
			on(func() { rowForce = "hover" })
			time.Sleep(400 * time.Millisecond)
			save(a.win, "f-config-hover")
			on(func() { rowForce = "" })
		}
	}
	mygo.RunOnMain(func() { a.win.SetSize(a.winWidth(), a.winHeight()) })
}
