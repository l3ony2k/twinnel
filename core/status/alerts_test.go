package status

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/tailscale"
)

func base(now time.Time) Snapshot {
	return Snapshot{Time: now, Loaded: true,
		Mihomo:    Mihomo{Reachable: true},
		Tailscale: Tailscale{Reachable: true, State: tailscale.State{BackendState: "Running"}}}
}

func keys(as []Alert) string {
	var k []string
	for _, a := range as {
		k = append(k, a.Key)
	}
	return strings.Join(k, ",")
}

func TestAlerterMihomoGraceAndDedupe(t *testing.T) {
	t0 := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	a := &Alerter{}
	down := func(at time.Time) Snapshot {
		s := base(at)
		s.Mihomo = Mihomo{Err: "connection refused"}
		return s
	}
	if got := a.Check(base(t0)); len(got) != 0 {
		t.Fatalf("healthy: %v", got)
	}
	if got := a.Check(down(t0.Add(time.Second))); len(got) != 0 {
		t.Fatalf("within grace: %v", got)
	}
	got := a.Check(down(t0.Add(20 * time.Second)))
	if keys(got) != "mihomo-down" || got[0].Body != "connection refused" {
		t.Fatalf("after grace: %v", got)
	}
	if got := a.Check(down(t0.Add(40 * time.Second))); len(got) != 0 {
		t.Fatalf("same episode again: %v", got)
	}
	// Recovers and goes down again within MinGap: swallowed.
	a.Check(base(t0.Add(time.Minute)))
	a.Check(down(t0.Add(2 * time.Minute)))
	if got := a.Check(down(t0.Add(3 * time.Minute))); len(got) != 0 {
		t.Fatalf("flap within min gap: %v", got)
	}
	// After MinGap a new episode fires again.
	a.Check(base(t0.Add(40 * time.Minute)))
	a.Check(down(t0.Add(41 * time.Minute)))
	if got := a.Check(down(t0.Add(42 * time.Minute))); keys(got) != "mihomo-down" {
		t.Fatalf("new episode: %v", got)
	}
}

func TestAlerterTailscale(t *testing.T) {
	t0 := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	a := &Alerter{}
	s := base(t0)
	s.Tailscale.State.BackendState = "NeedsLogin"
	if got := a.Check(s); keys(got) != "ts-login" {
		t.Fatalf("login: %v", got)
	}
	exp := t0.Add(48 * time.Hour)
	s = base(t0.Add(time.Minute))
	s.Tailscale.State.KeyExpiry = &exp
	got := a.Check(s)
	if len(got) != 1 || !strings.HasPrefix(got[0].Key, "ts-key/") {
		t.Fatalf("key expiry: %v", got)
	}
	far := t0.Add(10 * 24 * time.Hour)
	s.Tailscale.State.KeyExpiry = &far
	if got := a.Check(s); len(got) != 0 {
		t.Fatalf("far expiry: %v", got)
	}
}

func report(at time.Time, st health.Status) *health.Report {
	return &health.Report{Started: at, Checks: []health.CheckReport{
		{ID: "magicdns", Title: "MagicDNS 解析", Result: health.Result{Status: st, Summary: "broken"}},
		{ID: "vpn-conflict", Title: "VPN", Result: health.Result{Status: health.StatusOK}},
	}}
}

func TestAlerterHealthTransitions(t *testing.T) {
	t0 := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	a := &Alerter{}
	s := base(t0)
	// Failing in the first report: not a transition, no alert.
	s.Health.Report = report(t0, health.StatusFail)
	if got := a.Check(s); len(got) != 0 {
		t.Fatalf("first report: %v", got)
	}
	s.Health.Report = report(t0.Add(time.Minute), health.StatusWarn)
	s.Time = t0.Add(time.Minute)
	a.Check(s)
	s.Health.Report = report(t0.Add(2*time.Minute), health.StatusFail)
	s.Time = t0.Add(2 * time.Minute)
	got := a.Check(s)
	if keys(got) != "health/magicdns" || !strings.Contains(got[0].Title, "MagicDNS") {
		t.Fatalf("warn→fail: %v", got)
	}
	// The same report seen again (other snapshot changes): nothing new.
	s.Time = t0.Add(3 * time.Minute)
	if got := a.Check(s); len(got) != 0 {
		t.Fatalf("repeat: %v", got)
	}
}

func TestAlerterBurst(t *testing.T) {
	t0 := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	a := &Alerter{MaxBurst: 1}
	s := base(t0)
	s.Tailscale.State.BackendState = "NeedsLogin"
	exp := t0.Add(time.Hour)
	s.Tailscale.State.KeyExpiry = &exp
	if got := a.Check(s); len(got) != 1 {
		t.Fatalf("burst cap: %v", got)
	}
	s.Time = t0.Add(10 * time.Minute) // window passed: the held one goes out
	if got := a.Check(s); len(got) != 1 {
		t.Fatalf("after window: %v", got)
	}
}

func TestEvaluateHealth(t *testing.T) {
	now := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	s := base(now)
	s.Mihomo.Config.Tun.Enable = true
	s.Tailscale.Install.Kind = tailscale.InstallTailscaled
	s.Health.Report = report(now, health.StatusWarn)
	Evaluate(&s)
	if s.Level != LevelWarn || !strings.Contains(s.Reasons[0].Text, "1 项警告") {
		t.Fatalf("warn: %v %v", s.Level, s.Reasons)
	}
	s.Health.Report = report(now, health.StatusFail)
	Evaluate(&s)
	if s.Level != LevelError || !strings.Contains(s.Reasons[0].Text, "1 项失败") {
		t.Fatalf("fail: %v %v", s.Level, s.Reasons)
	}
}

func TestMonitorRunHealth(t *testing.T) {
	var runs atomic.Int32
	src := HealthFunc(func(ctx context.Context) (*health.Report, error) {
		runs.Add(1)
		return report(time.Now(), health.StatusWarn), nil
	})
	m := NewMonitor(Options{Mihomo: &fakeMihomo{}, Health: src, HealthInterval: -1})
	m.RunHealth(context.Background())
	h := m.Snapshot().Health
	if runs.Load() != 1 || h.Report == nil || h.Running {
		t.Fatalf("runs %d, health %+v", runs.Load(), h)
	}
	if w, f := h.Counts(); w != 1 || f != 0 {
		t.Fatalf("counts %d %d", w, f)
	}
}
