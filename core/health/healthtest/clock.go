// Package healthtest has test helpers for code that uses core/health.
package healthtest

import (
	"sync"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
)

// FakeClock is a health.Clock whose timers fire only when Advance is called.
type FakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	c        *FakeClock
	ch       chan time.Time
	deadline time.Time
	active   bool
}

// NewFakeClock returns a FakeClock at a fixed date.
func NewFakeClock() *FakeClock {
	return &FakeClock{now: time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)}
}

// Now returns the fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// NewTimer returns a timer due d from now.
func (c *FakeClock) NewTimer(d time.Duration) health.Timer {
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
func (c *FakeClock) Advance(d time.Duration) {
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

// Armed reports whether an active timer is due exactly d from now.
func (c *FakeClock) Armed(d time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.timers {
		if t.active && t.deadline.Equal(c.now.Add(d)) {
			return true
		}
	}
	return false
}

// WaitUntil polls cond for up to 2 seconds (real time).
func WaitUntil(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}
