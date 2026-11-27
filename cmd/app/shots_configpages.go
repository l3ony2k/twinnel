package main

import (
	"time"

	"github.com/egoist/mygo"
)

// configPagesShots (DUET_SHOTS_FLOW=configpages) walks round 3's
// config-page changes against a scratch helper with writes on
// (DUET_READ_ONLY=0, DUET_SHOTS_WRITE=1): a switch that marks its row
// 未应用, an editor note on its line, 删除覆写 with its 撤销 toast, the
// undo, 放弃改动 with its toast, and the in-sync apply card with 预览
// waiting. Nothing asks: the reversible actions no longer open a dialog.
func (a *app) configPagesShots(save func(*mygo.Window, string)) {
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
	on(func() {
		old := a.page
		a.page = pageConfig
		a.pageChanged(old)
	})
	wait(time.Second)

	// A switch: the row says 未应用 until 应用.
	var target string
	on(func() {
		for _, l := range a.hd.layers {
			if !l.Managed && l.Stage != stageBase {
				target = l.ID
				a.setLayerEnabled(l, !l.Enabled)
				break
			}
		}
	})
	wait(1500 * time.Millisecond)
	save(a.win, "cp02-pending-marks")

	// An editor note on its line: +.lan is read as a merge directive.
	on(func() {
		a.newLayer()
		v := &a.hd.cfg
		v.edID, v.edName = "lan-demo", "lan demo"
		v.ed.Buffer.Set("# synthetic\n+rules:\n  - DOMAIN-SUFFIX,app.example,DIRECT\nhosts:\n  +.lan: 192.0.2.10\n")
	})
	wait(800 * time.Millisecond)
	save(a.win, "cp03-editor-note")
	on(func() { a.hd.cfg.sel, a.hd.cfg.isNew, a.hd.cfg.ed = "", false, nil })

	// 删除覆写: at once, with 撤销 in the toast.
	var name string
	on(func() {
		if i := indexLayer(a.hd.layers, target); i >= 0 {
			name = layerTitle(a.hd.layers[i])
			a.removeLayer(target, name)
		}
	})
	time.Sleep(2500 * time.Millisecond)
	save(a.win, "cp04-delete-undo-toast")
	on(func() { a.undoLast(a.hd.cfg.undo) })
	wait(2 * time.Second)
	save(a.win, "cp05-undone")

	// 放弃改动: back to what mihomo runs, with 撤销 in the toast.
	on(func() {
		if last := lastApplied(a.hd.history); last != nil {
			a.discardChanges(*last)
		}
	})
	time.Sleep(2500 * time.Millisecond)
	save(a.win, "cp06-discard-undo-toast")
	wait(time.Second)

	// In sync: apply what the overrides compose, so 预览 and 应用 wait.
	// Last, so its toast does not sit over the undo toasts above (a
	// hidden shot window does not run the toasts' timers).
	on(func() { a.hd.cfg.reason = ""; a.runApply() })
	wait(2 * time.Second)
	on(func() { a.hd.cfg.applied = nil })
	wait(500 * time.Millisecond)
	save(a.win, "cp07-in-sync")
}
