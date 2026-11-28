package health_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/health/healthtest"
)

type varCheck struct {
	id     string
	status atomic.Value // Status
	runs   atomic.Int32
}

func newVarCheck(id string, s Status) *varCheck {
	c := &varCheck{id: id}
	c.status.Store(s)
	return c
}

func (c *varCheck) ID() string          { return c.id }
func (c *varCheck) Title() string       { return c.id }
func (c *varCheck) Platforms() []string { return AllPlatforms }
func (c *varCheck) Run(context.Context, Env) Result {
	c.runs.Add(1)
	return Result{Status: c.status.Load().(Status), Summary: c.id}
}

type platformEnv struct{ Env }

func (platformEnv) Platform() string { return Darwin }

func newTestScheduler(checks ...Check) (*Scheduler, *healthtest.FakeClock) {
	reg := NewRegistry()
	for _, c := range checks {
		reg.Register(c)
	}
	clk := healthtest.NewFakeClock()
	return &Scheduler{
		Runner:   &Runner{Registry: reg, Now: clk.Now},
		Env:      platformEnv{},
		Interval: time.Minute,
		Debounce: 2 * time.Second,
		Clock:    clk,
	}, clk
}

func recv(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
	return Event{}
}

func noEvent(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event %q", ev.Reason)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestSchedulerLifecycle(t *testing.T) {
	a := newVarCheck("a", StatusOK)
	b := newVarCheck("b", StatusWarn)
	s, clk := newTestScheduler(a, b)
	events, cancelSub := s.Subscribe(8)
	defer cancelSub()
	var callbacks atomic.Int32
	s.OnEvent = func(Event) { callbacks.Add(1) }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// Initial run: every check is a change from "".
	healthtest.WaitUntil(t, "start timer", func() bool { return clk.Armed(0) })
	clk.Advance(0)
	ev := recv(t, events)
	if ev.Reason != "start" || len(ev.Changes) != 2 || ev.Changes[0].From != "" || ev.Changes[1].To != StatusWarn {
		t.Fatalf("start event: %+v", ev)
	}
	if s.Last() != ev.Report || ev.Report.Overall != StatusWarn {
		t.Fatalf("last report not kept")
	}
	if err := s.Run(ctx); err != ErrSchedulerRunning {
		t.Errorf("second Run: %v", err)
	}

	// Interval run without changes: event with no Changes.
	healthtest.WaitUntil(t, "interval timer", func() bool { return clk.Armed(time.Minute) })
	clk.Advance(time.Minute)
	ev = recv(t, events)
	if ev.Reason != "interval" || len(ev.Changes) != 0 {
		t.Fatalf("interval event: %+v", ev)
	}

	// A burst of triggers is one debounced run; b goes warn → ok.
	b.status.Store(StatusOK)
	s.Trigger("network")
	s.Trigger("network")
	s.Trigger("dns")
	healthtest.WaitUntil(t, "debounce timer", func() bool { return clk.Armed(2 * time.Second) })
	clk.Advance(time.Second)
	noEvent(t, events)
	clk.Advance(time.Second)
	ev = recv(t, events)
	if ev.Reason != "network, dns" {
		t.Errorf("reason = %q", ev.Reason)
	}
	if len(ev.Changes) != 1 || ev.Changes[0].ID != "b" || ev.Changes[0].From != StatusWarn || ev.Changes[0].To != StatusOK {
		t.Errorf("changes = %+v", ev.Changes)
	}
	noEvent(t, events)

	// The triggered run restarted the interval.
	healthtest.WaitUntil(t, "interval restart", func() bool { return clk.Armed(time.Minute) })
	clk.Advance(59 * time.Second)
	noEvent(t, events)
	clk.Advance(time.Second)
	if ev = recv(t, events); ev.Reason != "interval" {
		t.Errorf("reason = %q", ev.Reason)
	}

	// RunNow runs synchronously and also emits.
	a.status.Store(StatusFail)
	rep := s.RunNow(ctx, "")
	if rep.Overall != StatusFail {
		t.Errorf("RunNow overall = %s", rep.Overall)
	}
	if ev = recv(t, events); ev.Reason != "manual" || len(ev.Changes) != 1 || ev.Changes[0].To != StatusFail {
		t.Errorf("manual event: %+v", ev)
	}

	cancel()
	if err := <-done; err != context.Canceled {
		t.Errorf("Run returned %v", err)
	}
	if n := callbacks.Load(); n != 5 {
		t.Errorf("OnEvent called %d times, want 5", n)
	}
	if n := a.runs.Load(); n != 5 {
		t.Errorf("check ran %d times, want 5", n)
	}
}

func TestSchedulerExternalTriggerAndNewEnv(t *testing.T) {
	a := newVarCheck("a", StatusOK)
	s, clk := newTestScheduler(a)
	s.SkipInitial = true
	envs := 0
	s.NewEnv = func() Env { envs++; return platformEnv{} }
	net := make(chan string)
	s.Triggers = []<-chan string{net}
	events, cancelSub := s.Subscribe(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	net <- "route changed"
	healthtest.WaitUntil(t, "debounce timer", func() bool { return clk.Armed(2 * time.Second) })
	clk.Advance(2 * time.Second)
	if ev := recv(t, events); ev.Reason != "route changed" {
		t.Errorf("reason = %q", ev.Reason)
	}
	if envs != 1 {
		t.Errorf("NewEnv called %d times", envs)
	}
	cancelSub()
	cancelSub() // idempotent
	if _, ok := <-events; ok {
		t.Error("channel not closed")
	}
	// No subscribers left: runs still work.
	if rep := s.RunNow(ctx, "x"); rep == nil || s.Last() != rep {
		t.Error("RunNow without subscribers")
	}
}

func TestDiff(t *testing.T) {
	prev := &Report{Checks: []CheckReport{
		{ID: "a", Result: Result{Status: StatusOK}},
		{ID: "gone", Result: Result{Status: StatusWarn}},
	}}
	cur := &Report{Checks: []CheckReport{
		{ID: "a", Result: Result{Status: StatusOK}},
		{ID: "new", Result: Result{Status: StatusFail}},
	}}
	got := Diff(prev, cur)
	if len(got) != 2 || got[0].ID != "new" || got[0].From != "" || got[0].To != StatusFail ||
		got[1].ID != "gone" || got[1].From != StatusWarn || got[1].To != "" {
		t.Errorf("Diff = %+v", got)
	}
	if len(Diff(cur, cur)) != 0 {
		t.Error("Diff of equal reports not empty")
	}
}

// SetLang words later runs in the new language and asks for one when the
// last report is in another; UseLang only sets it.
func TestSchedulerLang(t *testing.T) {
	s, _ := newTestScheduler(newVarCheck("a", StatusOK))
	ctx := context.Background()
	if rep := s.RunNow(ctx, ""); rep.Lang != DefaultLang {
		t.Fatalf("first run lang %q", rep.Lang)
	}
	if s.UseLang("en") != true {
		t.Error("UseLang(en) after a zh report should report a change")
	}
	if rep := s.RunNow(ctx, ""); rep.Lang != "en" {
		t.Errorf("run after UseLang: lang %q", rep.Lang)
	}
	if s.UseLang("en-GB") {
		t.Error("same language reported as a change")
	}
	s.SetLang("") // nothing
	if rep := s.RunNow(ctx, ""); rep.Lang != "en" {
		t.Errorf("SetLang(\"\") changed the language to %q", rep.Lang)
	}
}

// genCheck bumps a generation while it runs: a state change during a run.
type genCheck struct {
	varCheck
	gen *atomic.Uint64
}

func (c *genCheck) Run(ctx context.Context, e Env) Result {
	c.gen.Add(1)
	return c.varCheck.Run(ctx, e)
}

// A report carries the generation read when its run started, so a change
// during the run leaves it behind; Replace keeps the report's generation.
func TestSchedulerGen(t *testing.T) {
	var gen atomic.Uint64
	gen.Store(4)
	c := &genCheck{varCheck: varCheck{id: "a"}, gen: &gen}
	c.status.Store(StatusOK)
	s, _ := newTestScheduler(c, newVarCheck("b", StatusOK))
	s.Gen = gen.Load
	rep := s.RunNow(context.Background(), "test")
	if rep.Gen != 4 || gen.Load() != 5 {
		t.Fatalf("gen %d, state %d; want 4 (read before the checks), 5", rep.Gen, gen.Load())
	}
	rep = s.Replace(CheckReport{ID: "b", Result: Result{Status: StatusWarn}}, "egress")
	if rep == nil || rep.Gen != 4 || s.Last().Gen != 4 {
		t.Fatalf("replace: %+v", rep)
	}
	if rep := s.RunNow(context.Background(), "again"); rep.Gen != 5 {
		t.Fatalf("second run gen %d", rep.Gen)
	}
}
