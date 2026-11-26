package main

import (
	"testing"

	"github.com/l3ony2k/duet/core/rpc"
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
	if p.ConfirmHash == "" || p.State != planReady || len(p.Layers) != 3 || len(p.Candidates) != 2 || !p.Candidates[0].Chosen {
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
	if !takeoverEvent([]rpc.Event{{Type: eventTakeoverChanged}}) || takeoverEvent([]rpc.Event{{Type: rpc.EventKernelChanged}}) {
		t.Error("takeoverEvent")
	}
	if got := shortDigest("sha256:9b3d0c27a1e54f6b8c7d2e1f0a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b"); got != "9b3d0c27a1e54f6b…" {
		t.Errorf("shortDigest = %q", got)
	}
	if got := shortDigest("v3.30.0"); got != "v3.30.0" {
		t.Errorf("tag changed: %q", got)
	}
}
