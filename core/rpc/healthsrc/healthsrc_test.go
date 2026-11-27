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

// oldHelper rejects params it does not know, as a helper from before
// HealthLastParams.Lang does (DisallowUnknownFields).
type oldHelper struct {
	fakeCaller
	sawLang []string
}

func (o *oldHelper) Call(ctx context.Context, method string, params, result any) error {
	switch p := params.(type) {
	case rpc.HealthLastParams:
		o.sawLang = append(o.sawLang, p.Lang)
		return &rpc.Error{Code: rpc.CodeInvalidParams, Message: "invalid params"}
	case rpc.HealthRunParams:
		o.sawLang = append(o.sawLang, p.Lang)
		if p.Lang != "" {
			return &rpc.Error{Code: rpc.CodeInvalidParams, Message: "invalid params"}
		}
	}
	return o.fakeCaller.Call(ctx, method, params, result)
}

func TestSourceLangOldHelper(t *testing.T) {
	o := &oldHelper{}
	s := Source{Client: o, Lang: "en"}
	rep, err := s.Run(context.Background())
	if err != nil || rep == nil {
		t.Fatalf("run against an old helper: %v", err)
	}
	if o.runs != 1 || len(o.sawLang) != 3 || o.sawLang[0] != "en" || o.sawLang[1] != "en" || o.sawLang[2] != "" {
		t.Errorf("calls %v langs %q", o.calls, o.sawLang)
	}
}
