package main

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// configUIShots (TWINNEL_SHOTS_FLOW=configui) walks batch C's screens
// against a dev helper with fakes (TWINNEL_SOCKET, writes on): the 覆写 list
// in compose order, a save the lint holds back, a save the helper refuses
// (shown at its line), preview with the pending row and the 说明 filled
// in, apply, a failed mihomo -t and a failed reload (U53), history with
// its reasons, a rollback that drifts and the drift row, restoring the
// overrides, and 日志 → Twinnel. Confirm dialogs are never shown (they take
// the focus): their text goes to the log instead.
func (a *app) configUIShots(save func(*mygo.Window, string)) {
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
		time.Sleep(500 * time.Millisecond)
	}
	page := func(p string) {
		on(func() {
			old := a.page
			a.page = p
			a.pageChanged(old)
		})
		wait(800 * time.Millisecond)
	}
	scrollTop := func() {
		on(func() {
			if st := a.pageScroll[a.page]; st != nil {
				st.Y = 0
			}
		})
	}
	scrollEnd := func() {
		on(func() {
			if st := a.pageScroll[a.page]; st != nil {
				st.Y = 100000
			}
		})
		time.Sleep(400 * time.Millisecond)
	}
	closeEditor := func() { on(func() { a.hd.cfg.sel, a.hd.cfg.isNew, a.hd.cfg.ed = "", false, nil }) }
	newLayer := func(id, name, text string, format int) {
		on(func() {
			a.newLayer()
			v := &a.hd.cfg
			v.edID, v.edName, v.edFormat = id, name, format
			v.ed.Buffer.Set(text)
		})
		wait(500 * time.Millisecond)
	}

	page(pageConfig)
	save(a.win, "c01-config")

	// U35: the lint holds the save back (+.lan needs a list).
	newLayer("app3-demo", "app3 demo", "# app3 dev run (synthetic)\n+rules:\n  - DOMAIN-SUFFIX,app3.example,DIRECT\nhosts:\n  +.lan: 192.0.2.10\n", 0)
	save(a.win, "c02-save-blocked")
	// The helper's refusal, at its line: a Verge layer the lint does not read.
	newLayer("verge-demo", "verge demo", "# synthetic\nprepend-rules: DOMAIN,x.example,DIRECT\n", 1)
	on(a.saveLayer)
	wait(time.Second)
	save(a.win, "c03-save-refused")
	closeEditor()
	newLayer("app3-demo", "app3 demo", "# app3 dev run (synthetic)\n+rules:\n  - DOMAIN-SUFFIX,app3.example,DIRECT\nhosts:\n  '<+.lan>': 192.0.2.10\n", 0)
	on(a.saveLayer)
	wait(time.Second)
	closeEditor()

	// U39 / H6 / U33: pending row, preview with the order and the 说明.
	scrollEnd()
	save(a.win, "c04-pending")
	on(func() { a.runPreview(a.mon.Snapshot()) })
	wait(2 * time.Second)
	scrollEnd()
	save(a.win, "c05-preview")
	on(a.runApply)
	wait(2 * time.Second)
	scrollEnd()
	save(a.win, "c06-applied")

	// U53: mihomo -t fails; then the reload fails and the old config is
	// put back.
	for _, f := range []struct{ id, marker, shot string }{
		{"bad-check", "twinnel-test-invalid", "c07-apply-check-failed"},
		{"bad-reload", "twinnel-test-reload-fail", "c08-apply-reload-failed"},
	} {
		newLayer(f.id, f.id, "# synthetic\n+rules:\n  - DOMAIN,"+f.marker+".example,DIRECT\n", 0)
		on(a.saveLayer)
		wait(time.Second)
		closeEditor()
		on(a.runApply)
		wait(2 * time.Second)
		scrollEnd()
		save(a.win, f.shot)
		var l rpc.Layer
		on(func() {
			if i := indexLayer(a.hd.layers, f.id); i >= 0 {
				l = a.hd.layers[i]
			}
		})
		on(func() { a.setLayerEnabled(l, false) })
		wait(time.Second)
	}
	// Turn coexistence off and apply, so a rollback to the first entry
	// drifts (U41).
	on(func() {
		for _, l := range a.hd.layers {
			if l.ID == coexistLayerID {
				a.setLayerEnabled(l, false)
			}
		}
	})
	wait(time.Second)
	on(func() { a.hd.cfg.reason = ""; a.runApply() })
	wait(2 * time.Second)

	// U40: history.
	page(pageHistory)
	var hist []rpc.HistoryEntry
	mygo.RunOnMain(func() { hist = a.hd.history })
	if len(hist) > 0 {
		target := hist[len(hist)-1]
		for _, e := range hist { // the oldest applied entry: one with a manifest
			if e.Layers != nil {
				target = e
			}
		}
		on(func() { a.hd.hist.sel, a.hd.hist.reveal = target.ID, true })
		wait(500 * time.Millisecond)
		save(a.win, "c09-history")
		var body []string
		mygo.RunOnMain(func() { body = rollbackEffects(target.Layers, a.hd.layers) })
		log.Printf("rollback confirm for %s: %s", target.ID, strings.Join(body, " | "))
		on(func() { a.historyRollbackNow(target) })
		wait(2 * time.Second)
		save(a.win, "c10-history-rolled-back")

		page(pageConfig)
		scrollTop()
		wait(time.Second)
		save(a.win, "c11-config-drift")
		on(func() { a.restoreLayers(i18n.T.DriftRestore, i18n.T.DoneRestored, target.ID, nil) })
		wait(1500 * time.Millisecond)
		save(a.win, "c12-config-restored")
	}

	// U29: what Twinnel did.
	page(pageLogs)
	on(func() { a.logs.source = logsTwinnel })
	wait(1500 * time.Millisecond)
	save(a.win, "c13-logs-Twinnel")
	on(func() { a.logs.query = "应用" })
	if os.Getenv("TWINNEL_LANG") == "en" {
		on(func() { a.logs.query = "apply" })
	}
	wait(500 * time.Millisecond)
	save(a.win, "c14-logs-twinnel-search")
	on(func() { a.logs.query, a.logs.source = "", logsMihomo })
}
