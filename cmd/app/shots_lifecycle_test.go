package main

import (
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
)

// Without a takeover fixture there is no plan: the lifecycle flow skips
// its takeover shots instead of dereferencing nil (ui-review 8.4).
func TestShotPlanReadyWithoutPlan(t *testing.T) {
	a := &app{}
	a.hd.take.plans = []rpc.TakeoverPlan{{PlanID: "stale"}}
	if a.shotPlanReady() {
		t.Fatal("no plan reported ready")
	}
	if a.hd.take.plans != nil {
		t.Errorf("plans = %+v, want none", a.hd.take.plans)
	}
	a.hd.take.plan = &rpc.TakeoverPlan{PlanID: "p1", State: rpc.PlanAdopted}
	if !a.shotPlanReady() || a.hd.take.plan.State != rpc.PlanReady ||
		len(a.hd.take.plans) != 1 || a.hd.take.plans[0].PlanID != "p1" {
		t.Errorf("plan %+v, plans %+v", a.hd.take.plan, a.hd.take.plans)
	}
}

// The shot after 新建接管计划 is named after what the page shows; a run
// whose plan never came saves nothing rather than a second copy of the
// page under the plan's name (ui-review 8.5).
func TestHelperPlanShot(t *testing.T) {
	for _, c := range []struct {
		offered, started, plan bool
		msg, want              string
	}{
		{true, true, true, "", "takeover-helper-plan"},
		{true, true, false, "takeover.plan: no mihomo found", "takeover-helper-plan-failed"},
		{true, true, false, "", ""},
		{true, false, true, "", ""}, // a plan from before: not this request's
		// Twinnel manages the setup: no plan on the page, so no shot that
		// repeats takeover-helper (Windows QA round 5 T1).
		{false, true, false, "takeover.plan: managed", ""},
	} {
		name, why := helperPlanShot(c.offered, c.started, c.plan, c.msg)
		if name != c.want || (name == "") == (why == "") {
			t.Errorf("%+v: %q (%s)", c, name, why)
		}
	}
}
