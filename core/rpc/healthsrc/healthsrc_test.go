package healthsrc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

var _ status.HealthSource = Source{}

type fakeCaller struct {
	last  *health.Report
	runs  int
	calls []string
}

func (f *fakeCaller) Call(_ context.Context, method string, _, result any) error {
	f.calls = append(f.calls, method)
	var rep *health.Report
	switch method {
	case rpc.MethodHealthLast:
		rep = f.last
	case rpc.MethodHealthRun:
		f.runs++
		rep = &health.Report{Platform: "darwin", Overall: health.StatusWarn}
		f.last = rep
	default:
		return errors.New("unexpected " + method)
	}
	out := rpc.HealthReport{}
	if rep != nil {
		out.Report, _ = json.Marshal(rep)
	}
	*(result.(*rpc.HealthReport)) = out
	return nil
}

func TestSource(t *testing.T) {
	f := &fakeCaller{}
	s := Source{Client: f}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		rep, err := s.Run(ctx)
		if err != nil || rep == nil || rep.Overall != health.StatusWarn {
			t.Fatalf("run %d: %+v %v", i, rep, err)
		}
	}
	if f.runs != 1 {
		t.Errorf("runs %d, want 1 (second Run reads health.last)", f.runs)
	}
	if _, err := (Source{Client: f, Fresh: true}).Run(ctx); err != nil || f.runs != 2 {
		t.Errorf("fresh: %v runs %d", err, f.runs)
	}
	if _, err := (Source{}).Run(ctx); err == nil {
		t.Error("no client accepted")
	}
}
