package main

import (
	"log"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// takeoverE2E drives the takeover wizard end to end against a development
// helper (TWINNEL_SHOTS_FLOW=takeover, with TWINNEL_SOCKET, TWINNEL_READ_ONLY=0):
// plan → candidates → re-plan with another base → plan with the best base →
// adopt (health before/after) → rollback to the pre-adopt file via 历史 →
// controller.secure, then checks the monitor still reaches the controller
// with the helper's credentials. It uses the page handlers, skipping only
// the confirm dialogs. Never run it against the live setup: the helper's
// probe must point at a copy (--takeover-services-dir and friends).
func (a *app) takeoverE2E(save func(*mygo.Window, string)) {
	on := func(fn func()) { mygo.RunOnMain(func() { fn(); a.invalidate() }) }
	idle := func(d, max time.Duration) {
		time.Sleep(d)
		deadline := time.Now().Add(max)
		for time.Now().Before(deadline) {
			busy := true
			mygo.RunOnMain(func() { busy = a.hd.busy != "" || a.hd.loading || a.hd.take.loading || a.creds.loading })
			if !busy {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		time.Sleep(600 * time.Millisecond)
	}
	shot := func(section, name string) {
		on(func() { a.shotSection = section })
		time.Sleep(600 * time.Millisecond)
		save(a.win, name)
	}
	page := func(p string) {
		on(func() {
			old := a.page
			a.page = p
			a.pageChanged(old)
		})
		idle(time.Second, 20*time.Second)
	}
	report := func(step string) {
		mygo.RunOnMain(func() {
			s := a.mon.Snapshot()
			c := a.creds.cur
			src, secret := "cfg", false
			if c != nil {
				src, secret = "helper/"+c.Source, c.Secret != ""
			}
			// Booleans only: never the secret.
			log.Printf("e2e %s: credentials from %s (secret set %v); mihomo reachable %v unauthorized %v err %q; op %q err %v",
				step, src, secret, s.Mihomo.Reachable, s.Mihomo.Unauthorized, s.Mihomo.Err, a.hd.msg, a.hd.msgErr)
		})
	}

	page(pageTakeover)
	report("start")
	shot("", "e2e-01-intro")

	on(func() { a.takeoverMakePlan("") })
	idle(2*time.Second, 3*time.Minute)
	var plan *rpc.TakeoverPlan
	mygo.RunOnMain(func() { plan = a.hd.take.plan })
	if plan == nil {
		report("plan failed")
		return
	}
	log.Printf("e2e plan %s: %d candidates, base %s, equivalent %v, validated %v", plan.PlanID, len(plan.Candidates), plan.Base, plan.Equivalent, plan.Validated)
	shot("plan", "e2e-02-plan-candidates")

	// Review: re-plan with another candidate, as 改用这个 does.
	other := ""
	for _, c := range plan.Candidates {
		if !c.Chosen && c.Error == "" {
			other = c.Path
			break
		}
	}
	best := plan.Base
	if other != "" {
		on(func() { a.takeoverMakePlan(other) })
		idle(2*time.Second, 3*time.Minute)
		mygo.RunOnMain(func() { plan = a.hd.take.plan })
		log.Printf("e2e re-plan with another base: %s equivalent %v", plan.Base, plan.Equivalent)
		shot("plan", "e2e-03-other-base")
		shot("verdict", "e2e-04-other-base-verdict")
		on(func() { a.takeoverMakePlan(best) })
		idle(2*time.Second, 3*time.Minute)
		mygo.RunOnMain(func() { plan = a.hd.take.plan })
	}
	shot("verdict", "e2e-05-verdict")
	shot("", "e2e-06-plan-list")

	on(func() { a.takeoverAdopt(*plan) })
	idle(2*time.Second, 5*time.Minute)
	var res *rpc.AdoptResult
	mygo.RunOnMain(func() { res = a.hd.take.adopt })
	if res == nil {
		report("adopt failed")
		shot("", "e2e-07-adopt-error")
		return
	}
	log.Printf("e2e adopt: ok %v rolled back %v history %s backup %s worse %v", res.OK, res.RolledBack, res.HistoryID, res.BackupHistoryID, res.Worse)
	idle(time.Second, 20*time.Second)
	report("after adopt")
	shot("result", "e2e-07-adopt-result")

	// Rollback through 历史 to the file as it was before adopt.
	page(pageHistory)
	shot("", "e2e-08-history")
	var target *rpc.HistoryEntry
	mygo.RunOnMain(func() {
		for _, e := range a.hd.history {
			if e.ID == res.BackupHistoryID {
				target = &e
			}
		}
	})
	if target != nil {
		on(func() { a.historyRollbackNow(*target) })
		idle(2*time.Second, 2*time.Minute)
		report("after rollback")
		shot("", "e2e-09-history-rollback")
	} else {
		log.Printf("e2e: backup history entry %s not listed", res.BackupHistoryID)
	}

	// controller.secure: the app must keep reaching the controller.
	page(pageTakeover)
	on(a.controllerSecure)
	idle(2*time.Second, 2*time.Minute)
	idle(3*time.Second, 20*time.Second) // compose.applied → credentials → monitor restart
	report("after secure")
	shot("result", "e2e-10-secure")
	page(pageOverview)
	time.Sleep(3 * time.Second)
	report("overview")
	shot("", "e2e-11-overview-after-secure")
	page(pageSettings)
	shot("", "e2e-12-settings-credentials")
	on(func() { a.shotSection = "" })
	var snap status.Snapshot
	mygo.RunOnMain(func() { snap = a.mon.Snapshot() })
	if snap.Mihomo.Unauthorized {
		log.Print("e2e FAIL: unauthorized after controller.secure")
	} else {
		log.Print("e2e: controller still authorized after controller.secure")
	}
}
