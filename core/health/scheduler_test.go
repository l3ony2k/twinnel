package health

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock fires timers only when Advance is called.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	c        *fakeClock
	ch       chan time.Time
	deadline time.Time
	active   bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, ch: make(chan time.Time, 1), deadline: c.now.Add(d), active: true}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }
func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := t.active
	t.active = false
	return was
}
func (t *fakeTimer) Reset(d time.Duration) {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	t.deadline, t.active = t.c.now.Add(d), true
}

// Advance moves time forward and fires due timers.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	for _, t := range c.timers {
		if t.active && !t.deadline.After(c.now) {
			t.active = false
			select {
			case t.ch <- c.now:
			default:
			}
		}
	}
}

// armed reports whether an active timer is due exactly d from now.
func (c *fakeClock) armed(d time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.timers {
		if t.active && t.deadline.Equal(c.now.Add(d)) {
			return true
		}
	}
	return false
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

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

func newTestScheduler(checks ...Check) (*Scheduler, *fakeClock) {
	reg := NewRegistry()
	for _, c := range checks {
		reg.Register(c)
	}
	clk := newFakeClock()
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
	waitUntil(t, "start timer", func() bool { return clk.armed(0) })
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
	waitUntil(t, "interval timer", func() bool { return clk.armed(time.Minute) })
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
	waitUntil(t, "debounce timer", func() bool { return clk.armed(2 * time.Second) })
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
	waitUntil(t, "interval restart", func() bool { return clk.armed(time.Minute) })
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
	waitUntil(t, "debounce timer", func() bool { return clk.armed(2 * time.Second) })
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
