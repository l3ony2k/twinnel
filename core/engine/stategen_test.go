package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/health/healthtest"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/sysdns"
)

// switchManager is a service manager whose mihomo can be stopped and
// started behind the helper's back (Stop-Service, a crash, launchctl by
// hand).
type switchManager struct {
	fakeManager
	mu      sync.Mutex
	running bool
	pid     int
}

func (m *switchManager) set(running bool, pid int) {
	m.mu.Lock()
	m.running, m.pid = running, pid
	m.mu.Unlock()
}

func (m *switchManager) Status(_ context.Context, l string) (service.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return service.Status{Label: l, Installed: true, Loaded: true, Running: m.running, PID: m.pid}, nil
}

// current runs the checks now and asserts health.last then says the
// report is current; it returns the generation.
func current(t *testing.T, h *harness) uint64 {
	t.Helper()
	ctx := context.Background()
	if _, err := h.e.HealthRun(ctx, "test", ""); err != nil {
		t.Fatal(err)
	}
	hr, err := h.e.HealthLast(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !hr.Fresh() || hr.Gen == 0 || hr.Gen != hr.StateGen {
		t.Fatalf("after a run: gen %d, state %d", hr.Gen, hr.StateGen)
	}
	return hr.StateGen
}

// stale asserts that health.last now says the report predates a state
// change (what doctor and MCP doctor go by).
func stale(t *testing.T, h *harness, what string) {
	t.Helper()
	hr, err := h.e.HealthLast(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if hr.Fresh() {
		t.Fatalf("%s: the report is still current (gen %d, state %d)", what, hr.Gen, hr.StateGen)
	}
}

// Every kind of state change the helper sees makes the last report stale:
// its own operations and what happens behind its back.
func TestStateGenTriggers(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	newHealth(h)
	svc := &switchManager{running: true, pid: 4242}
	h.e.Services = svc
	owners := &portOwners{}
	h.e.PortOwner = owners.lookup
	owners.set(h.ctlPort(), h.managedProc())
	d := &fakeDNS{svcs: []sysdns.Service{{Name: "Wi-Fi"}, {Name: "Ethernet", Servers: []string{"192.0.2.1"}}}}
	h.e.SystemDNS = d
	var dnsDown atomic.Bool
	h.e.DNSProbe = func(context.Context, string) error {
		if dnsDown.Load() {
			return errors.New("connection refused")
		}
		return nil
	}
	now := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
	var clock sync.Mutex
	h.e.Now = func() time.Time { clock.Lock(); defer clock.Unlock(); return now }
	advance := func(d time.Duration) { clock.Lock(); now = now.Add(d); clock.Unlock() }

	// A first look at mihomo is the baseline, not a change.
	if _, err := h.e.Status(ctx); err != nil {
		t.Fatal(err)
	}
	g := current(t, h)
	if _, err := h.e.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if hr, _ := h.e.HealthLast(ctx, ""); !hr.Fresh() || hr.StateGen != g {
		t.Fatalf("nothing changed, yet gen %d -> %d", g, hr.StateGen)
	}

	steps := []struct {
		name   string
		change func()
	}{
		{"apply", func() { h.apply("state gen") }},
		{"service op", func() {
			h.e.ServiceRunner = &ServiceRunner{Exec: &recExec{}, Files: &recFiles{}}
			if _, err := h.e.ServiceOp(ctx, "mihomo", service.OpRestart); err != nil {
				t.Fatal(err)
			}
		}},
		{"service.changed (host refresh, harden)", func() {
			h.e.publish(rpc.EventServiceChanged, map[string]any{"name": "mihomo", "op": "host-refresh"})
		}},
		{"mihomo stopped outside the helper, seen by status.get", func() {
			svc.set(false, 0)
			if _, err := h.e.Status(ctx); err != nil {
				t.Fatal(err)
			}
		}},
		{"mihomo started outside the helper, seen by health.last", func() {
			svc.set(true, 5151) // health.last itself looks
		}},
		{"controller unreachable, seen by status.get", func() {
			h.ctrl.SetDown(true)
			if st, _ := h.e.Status(ctx); st.Mihomo.Reachable {
				t.Fatal("the controller still answers")
			}
		}},
		{"controller back, seen by health.last", func() { h.ctrl.SetDown(false) }},
		{"controller owner changes", func() {
			owners.set(h.ctlPort(), clashPartyProc())
			h.e.ControllerOwner(ctx)
		}},
		{"dns point", func() {
			owners.set(h.ctlPort(), h.managedProc())
			if _, err := h.e.DNSPoint(ctx); err != nil {
				t.Fatal(err)
			}
		}},
		{"dns watchdog restores", func() {
			h.e.ServiceRunner = nil
			svc.set(false, 0)
			h.e.Status(ctx) // the service change is its own step above
			current(t, h)
			dnsDown.Store(true)
			w := h.e.dnsWatchdog()
			for range 8 {
				w.Tick(ctx)
				advance(5 * time.Second)
			}
			if !h.e.DNSWatch().Restored() {
				t.Fatal("the watchdog did not restore")
			}
		}},
		{"dns watchdog re-points", func() {
			svc.set(true, 6161)
			h.e.Status(ctx)
			current(t, h)
			dnsDown.Store(false)
			w := h.e.dnsWatchdog()
			for range 40 {
				w.Tick(ctx)
				advance(5 * time.Second)
				if !h.e.DNSWatch().Restored() {
					break
				}
			}
			if h.e.DNSWatch().Restored() {
				t.Fatal("the watchdog did not point back")
			}
		}},
		{"dns restore", func() {
			if _, err := h.e.DNSRestore(ctx); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, s := range steps {
		before := current(t, h)
		s.change()
		stale(t, h, s.name)
		if after := current(t, h); after <= before {
			t.Fatalf("%s: gen %d -> %d", s.name, before, after)
		}
	}
}

// A change seen outside a helper operation also asks for a run, so the
// report the app's banner and status summary read catches up by itself.
func TestStateChangeRunsChecks(t *testing.T) {
	h := newHarness(t)
	chk, _ := newHealth(h)
	svc := &switchManager{running: true, pid: 4242}
	h.e.Services = svc
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { h.e.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	h.e.Status(ctx) // baseline
	healthtest.WaitUntil(t, "quiet", func() bool { return true })
	n := chk.count()
	svc.set(false, 0)
	h.e.Status(ctx)
	healthtest.WaitUntil(t, "a health run after mihomo stopped", func() bool { return chk.count() > n })
	healthtest.WaitUntil(t, "a current report", func() bool {
		hr, err := h.e.HealthLast(ctx, "")
		return err == nil && hr.Fresh()
	})
}
