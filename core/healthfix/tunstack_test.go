package healthfix

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// The tun-dataplane fix adds the gvisor override, applies, and the undo
// removes it again.
func TestTUNStackFix(t *testing.T) {
	ctx := context.Background()
	h := newFakeHelper()
	f := &Fixer{Helper: h, Lang: "en", Platform: health.Windows}
	cr := health.CheckReport{ID: "tun-dataplane", Result: health.Result{Status: health.StatusFail, Reasons: []string{health.ReasonDataplaneTCP}}}
	p, err := f.Plan(ctx, cr, health.ReasonDataplaneTCP)
	if err != nil || p.Action != health.FixTUNStack || !p.Undo || !strings.Contains(strings.Join(p.Steps, "\n"), "gvisor") {
		t.Fatalf("plan %+v, %v", p, err)
	}
	out, err := f.Apply(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := h.layer(health.TUNStackLayerID); !strings.Contains(c, "stack: gvisor") {
		t.Fatalf("layer %q", c)
	}
	if !slices.Contains(h.calls, rpc.MethodComposeApply) || out.Undo == nil || len(out.Undo.Layers) != 1 || !out.Undo.Layers[0].Absent {
		t.Fatalf("calls %v undo %+v", h.calls, out.Undo)
	}
	if _, err := f.Undo(ctx, out.Undo); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.layer(health.TUNStackLayerID); ok {
		t.Fatal("undo left the override")
	}

	// A failed apply removes the override it added.
	h = newFakeHelper()
	h.failOn = rpc.MethodComposeApply
	f.Helper = h
	if _, err := f.Apply(ctx, p); err == nil {
		t.Fatal("apply error lost")
	}
	if _, ok := h.layer(health.TUNStackLayerID); ok {
		t.Fatal("failed fix left the override")
	}
}

// The gvisor stack already: the fix is a restart, not another override.
func TestTUNStackGvisorRestarts(t *testing.T) {
	spec, ok := health.FixFor(health.ReasonDataplaneGvisor, health.Darwin)
	if !ok || spec.Action != health.FixRestartMihomo {
		t.Fatalf("%+v", spec)
	}
}
