package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// The 接管 page: take over the existing mihomo setup. The helper inspects
// it read-only and writes a plan (takeover.plan); the page shows the plan
// (base subscription and candidates, layers, deliberate changes, the
// semantic-diff verdict); 接管 runs takeover.adopt with the plan's confirm
// hash (backup → import → apply → health before/after → automatic
// rollback when worse). 加固控制接口 runs controller.secure.

type takeoverView struct {
	plans   []rpc.TakeoverPlan // takeover.plans, newest first
	loaded  bool
	loading bool
	// unsupported: the helper has no takeover.* yet.
	unsupported bool
	listErr     string

	plan     *rpc.TakeoverPlan // the selected plan, full (summary + semdiff)
	showText bool              // summary and semdiff texts expanded
	showFull bool              // the semdiff with every dead rule (SemDiffFull)

	adopt  *rpc.AdoptResult
	secure *rpc.ControllerSecureResult

	// fixture: the plan and result come from TWINNEL_TAKEOVER_FIXTURE (a
	// recorded plan.json / adopt.json, for screenshots); nothing is sent.
	fixture bool

	// Disclosures of the plan view (takeoverview.go).
	candOpen, othersOpen bool
	keysOpen             map[string]bool // change area → its keys shown
	rulesOpen            bool
	impactOpen           bool

	autostart autostartView // takeover_autostart.go
}

// isTooOld reports whether err means the helper lacks the method.
func isTooOld(err error) bool {
	var re *rpc.Error
	return errors.As(err, &re) && (re.Code == rpc.CodeMethodNotFound || re.Code == rpc.CodeNotImplemented)
}

// loadTakeover lists the plans. Main thread.
func (a *app) loadTakeover() {
	v := &a.hd.take
	if v.fixture || !a.hd.connected || v.loading {
		return
	}
	v.loading = true
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var plans []rpc.TakeoverPlan
		err := hc.Call(ctx, rpc.MethodTakeoverPlans, rpc.TakeoverPlansParams{}, &plans)
		mygo.RunOnMain(func() {
			v.loading, v.loaded = false, true
			v.unsupported = isTooOld(err)
			v.listErr = ""
			if err != nil && !v.unsupported {
				v.listErr = helperError(err)
			}
			v.plans = plans
			// Keep the selection fresh (its state changes after adopt).
			if v.plan != nil {
				for _, p := range plans {
					if p.PlanID == v.plan.PlanID {
						v.plan.State, v.plan.AdoptedAt, v.plan.Backup = p.State, p.AdoptedAt, p.Backup
					}
				}
			}
			a.invalidate()
		})
	}()
}

// loadTakeoverFixture fills the page from dir/plan.json (and
// dir/adopt.json when present). Development aid.
func (a *app) loadTakeoverFixture(dir string) error {
	v := &a.hd.take
	var p rpc.TakeoverPlan
	b, err := os.ReadFile(filepath.Join(dir, "plan.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return fmt.Errorf("plan.json: %w", err)
	}
	v.fixture, v.loaded, v.unsupported, v.listErr = true, true, false, ""
	v.plans = []rpc.TakeoverPlan{p}
	v.plan = &p
	if b, err := os.ReadFile(filepath.Join(dir, "adopt.json")); err == nil {
		var r rpc.AdoptResult
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("adopt.json: %w", err)
		}
		v.adopt = &r
	}
	return nil
}

// takeoverMakePlan asks the helper for a new plan; base picks one of the
// candidates it found (empty: its best).
func (a *app) takeoverMakePlan(base string) {
	T := i18n.T
	v := &a.hd.take
	var p rpc.TakeoverPlan
	a.helperOp(T.TakeoverPlan, 3*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodTakeoverPlan, rpc.TakeoverPlanParams{Base: base}, &p)
	}, func(err error) {
		if isTooOld(err) {
			v.unsupported = true
		}
		if err == nil {
			v.plan, v.adopt, v.showText, v.showFull = &p, nil, false, false
		}
		a.loadTakeover()
	})
}

// takeoverSelect loads one plan in full (with its summary and semdiff).
func (a *app) takeoverSelect(id string) {
	T := i18n.T
	v := &a.hd.take
	var ps []rpc.TakeoverPlan
	a.helperOp(T.TakeoverOpen, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodTakeoverPlans, rpc.TakeoverPlansParams{PlanID: id}, &ps)
	}, func(err error) {
		if err == nil && len(ps) == 1 {
			v.plan, v.adopt, v.showText = &ps[0], nil, false
		}
	})
}

func (a *app) takeoverAdopt(p rpc.TakeoverPlan) {
	T := i18n.T
	v := &a.hd.take
	var r rpc.AdoptResult
	a.helperOp(T.TakeoverAdopt, 10*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodTakeoverAdopt, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash}, &r)
	}, func(err error) {
		if err == nil {
			v.adopt = &r
			if !r.OK {
				a.hd.msg, a.hd.msgErr = T.RPCErrorText(0, r.Error), true
			}
		}
		a.loadTakeover()
		a.refreshCredentials()
		a.runHealth()
	})
}

func (a *app) controllerSecure() {
	T := i18n.T
	v := &a.hd.take
	var r rpc.ControllerSecureResult
	a.helperOp(T.SecureRun, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodControllerSecure, nil, &r)
	}, func(err error) {
		if isTooOld(err) {
			v.unsupported = true
		}
		v.secure = nil
		if err == nil {
			v.secure = &r
		}
		a.refreshCredentials() // the new secret, without waiting for the event
	})
}

// takeoverEvent reports whether a batch changes the plan list.
func takeoverEvent(evs []rpc.Event) bool {
	for _, e := range evs {
		if e.Type == rpc.EventTakeoverChanged || e.Type == rpc.EventComposeApplied {
			return true
		}
	}
	return false
}
