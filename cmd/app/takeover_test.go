package main

import (
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
)

func TestTakeoverFixture(t *testing.T) {
	a := &app{}
	if err := a.loadTakeoverFixture("testdata/takeover"); err != nil {
		t.Fatal(err)
	}
	v := a.hd.take
	if !v.fixture || v.plan == nil || v.adopt == nil {
		t.Fatalf("fixture not loaded: %+v", v)
	}
	p := v.plan
	if p.ConfirmHash == "" || p.State != rpc.PlanReady || len(p.Layers) != 3 || len(p.Candidates) != 2 || !p.Candidates[0].Chosen {
		t.Errorf("plan decoded wrong: %+v", p)
	}
	r := v.adopt
	if !r.RolledBack || len(r.Worse) != 1 || len(r.HealthAfterRollback) != 5 || len(r.Steps) != 6 {
		t.Errorf("adopt decoded wrong: %+v", r)
	}
	if err := a.loadTakeoverFixture("testdata/none"); err == nil {
		t.Error("missing fixture accepted")
	}
}

func TestTakeoverHelpers(t *testing.T) {
	if !isTooOld(&rpc.Error{Code: rpc.CodeMethodNotFound}) || !isTooOld(&rpc.Error{Code: rpc.CodeNotImplemented}) {
		t.Error("-32601/-32005 should mean too old")
	}
	if isTooOld(&rpc.Error{Code: rpc.CodeFailed}) || isTooOld(nil) {
		t.Error("other errors are not too old")
	}
	if !takeoverEvent([]rpc.Event{{Type: rpc.EventTakeoverChanged}}) || takeoverEvent([]rpc.Event{{Type: rpc.EventKernelChanged}}) {
		t.Error("takeoverEvent")
	}
}
