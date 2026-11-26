// Package healthsrc reads health reports from the helper over RPC. Source
// implements core/status.HealthSource (Run(ctx) (*health.Report, error)),
// so the app can swap its in-process runner for the helper's scheduler.
package healthsrc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/rpc"
)

// Caller is the part of *rpc.Client the source needs. An app that
// reconnects to the helper can pass its own wrapper.
type Caller interface {
	Call(ctx context.Context, method string, params, result any) error
}

// Source answers Run from the helper.
//
// By default Run returns the helper's newest report (health.last), which
// the helper keeps fresh on its own schedule, on network changes and after
// every apply; only before the helper's first run does it ask for a run.
// With Fresh set, every Run asks the helper to run the checks now
// (health.run), which is what a "重新检查" button wants; RunNow does that
// regardless of Fresh.
type Source struct {
	Client Caller
	Fresh  bool
	// Reason is sent with health.run (default "app").
	Reason string
}

// ErrNoReport is returned when the helper has no report and could not run.
var ErrNoReport = errors.New("helper returned no health report")

// Run implements status.HealthSource.
func (s Source) Run(ctx context.Context) (*health.Report, error) {
	if s.Fresh {
		return s.RunNow(ctx)
	}
	rep, err := s.Last(ctx)
	if err != nil || rep != nil {
		return rep, err
	}
	return s.RunNow(ctx)
}

// Last returns the helper's newest report, or nil before its first run.
func (s Source) Last(ctx context.Context) (*health.Report, error) {
	var r rpc.HealthReport
	if err := s.call(ctx, rpc.MethodHealthLast, nil, &r); err != nil {
		return nil, err
	}
	return Decode(r)
}

// RunNow asks the helper to run every check now and returns the report.
func (s Source) RunNow(ctx context.Context) (*health.Report, error) {
	reason := s.Reason
	if reason == "" {
		reason = "app"
	}
	var r rpc.HealthReport
	if err := s.call(ctx, rpc.MethodHealthRun, rpc.HealthRunParams{Reason: reason}, &r); err != nil {
		return nil, err
	}
	rep, err := Decode(r)
	if err == nil && rep == nil {
		err = ErrNoReport
	}
	return rep, err
}

func (s Source) call(ctx context.Context, method string, params, result any) error {
	if s.Client == nil {
		return errors.New("healthsrc: no helper client")
	}
	return s.Client.Call(ctx, method, params, result)
}

// Decode turns the report of health.last / health.run back into a
// health.Report; nil when the helper had none.
func Decode(r rpc.HealthReport) (*health.Report, error) {
	if len(r.Report) == 0 || string(r.Report) == "null" {
		return nil, nil
	}
	var rep health.Report
	if err := json.Unmarshal(r.Report, &rep); err != nil {
		return nil, fmt.Errorf("decode health report: %w", err)
	}
	return &rep, nil
}

// DecodeChange decodes the data of a check.changed event.
func DecodeChange(ev rpc.Event) (health.Change, error) {
	var c health.Change
	err := json.Unmarshal(ev.Data, &c)
	return c, err
}
