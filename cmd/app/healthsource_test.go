package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

type fakeHelper struct {
	state status.HelperState
	noRPC bool // an old helper without health.*
	calls []string
}

func (f *fakeHelper) State() status.Helper { return status.Helper{State: f.state} }

func (f *fakeHelper) Call(_ context.Context, method string, _, result any) error {
	f.calls = append(f.calls, method)
	if f.noRPC {
		return &rpc.Error{Code: rpc.CodeMethodNotFound, Message: "unknown method"}
	}
	b, _ := json.Marshal(&health.Report{Platform: "helper", Overall: health.StatusOK})
	*(result.(*rpc.HealthReport)) = rpc.HealthReport{Report: b}
	return nil
}

func TestHelperHealth(t *testing.T) {
	ctx := context.Background()
	localRuns := 0
	local := status.HealthFunc(func(context.Context) (*health.Report, error) {
		localRuns++
		return &health.Report{Platform: "local"}, nil
	})
	f := &fakeHelper{state: status.HelperConnected}
	h := &helperHealth{hc: f, local: local}

	rep, err := h.Run(ctx)
	if err != nil || rep.Platform != "helper" || !h.FromHelper() || localRuns != 0 {
		t.Fatalf("connected: %+v %v from=%v local=%d", rep, err, h.FromHelper(), localRuns)
	}
	if f.calls[0] != rpc.MethodHealthLast {
		t.Errorf("default run should read health.last, got %v", f.calls)
	}
	f.calls = nil
	h.fresh.Store(true)
	h.Run(ctx)
	if len(f.calls) != 1 || f.calls[0] != rpc.MethodHealthRun {
		t.Errorf("rerun should call health.run once, got %v", f.calls)
	}
	if h.fresh.Load() {
		t.Error("fresh not consumed")
	}

	f.state = status.HelperNotRunning
	if rep, _ := h.Run(ctx); rep.Platform != "local" || h.FromHelper() || localRuns != 1 {
		t.Errorf("not connected: %+v local=%d", rep, localRuns)
	}

	f.state, f.noRPC = status.HelperConnected, true
	if rep, _ := h.Run(ctx); rep.Platform != "local" || localRuns != 2 {
		t.Errorf("old helper: %+v local=%d", rep, localRuns)
	}

	if wrapHealth(nil, nil) != nil {
		t.Error("nil local should stay nil")
	}
	if w := wrapHealth(nil, local); w.hc != nil {
		t.Error("nil conn should leave hc nil (not a typed nil)")
	}
}

func TestHealthEvent(t *testing.T) {
	if healthEvent([]rpc.Event{{Type: rpc.EventComposeApplied}}) {
		t.Error("compose.applied is not a health event")
	}
	if !healthEvent([]rpc.Event{{Type: rpc.EventStateChanged}, {Type: rpc.EventHealthUpdated}}) {
		t.Error("health.updated missed")
	}
}
