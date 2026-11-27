package status

import (
	"context"
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/tailscale"
)

// Before takeover, health failures are findings: a warning, with the
// same one number the badge shows.
func TestEvaluateFindingsBeforeTakeover(t *testing.T) {
	now := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	s := base(now)
	s.Mihomo.Config.Tun.Enable = true
	s.Tailscale.Install.Kind = tailscale.InstallTailscaled
	s.Helper.State = HelperNotInstalled
	s.Health.Report = report(now, health.StatusFail)
	Evaluate(&s)
	if s.Level != LevelWarn || s.Reasons[0].Code != ReasonHealth {
		t.Fatalf("not taken over: %v %+v", s.Level, s.Reasons)
	}
	if n, l := s.HealthBadge(); n != 1 || l != LevelWarn {
		t.Errorf("badge %d %v", n, l)
	}
	// Taken over: the same failure is a fault.
	s.Helper = Helper{State: HelperConnected, Status: &rpc.Status{LastApply: &rpc.ApplyResult{}}}
	Evaluate(&s)
	if s.Level != LevelError {
		t.Fatalf("taken over: %v %+v", s.Level, s.Reasons)
	}
	if n, l := s.HealthBadge(); n != 1 || l != LevelError {
		t.Errorf("badge %d %v", n, l)
	}
}

// The badge counts failures only when there are some: 3 fail + 2 warn
// reads 3, as the headline does.
func TestHealthBadgeMatchesHeadline(t *testing.T) {
	now := time.Now()
	s := base(now)
	s.Mihomo.Config.Tun.Enable = true
	s.Tailscale.Install.Kind = tailscale.InstallTailscaled
	rep := &health.Report{Started: now}
	for i, st := range []health.Status{health.StatusFail, health.StatusFail, health.StatusFail, health.StatusWarn, health.StatusWarn} {
		rep.Checks = append(rep.Checks, health.CheckReport{ID: string(rune('a' + i)), Result: health.Result{Status: st}})
	}
	s.Health.Report = rep
	Evaluate(&s)
	if n, _ := s.HealthBadge(); n != 3 {
		t.Errorf("badge %d, want 3", n)
	}
}

func TestMonitorAcceptsExitNode(t *testing.T) {
	rep := &health.Report{Started: time.Now(), Checks: []health.CheckReport{
		{ID: "vpn-conflict", Result: health.Result{Status: health.StatusFail, Causes: []string{health.CauseExitNode}}},
		{ID: "tailnet-route", Result: health.Result{Status: health.StatusFail}},
	}}
	src := HealthFunc(func(context.Context) (*health.Report, error) { return rep, nil })
	m := NewMonitor(Options{Mihomo: &fakeMihomo{}, Health: src, HealthInterval: -1, ExitNodeAccepted: "exit-a"})
	m.update(func(s *Snapshot) {
		s.Tailscale = Tailscale{Reachable: true, State: tailscale.State{BackendState: "Running", ExitNode: &tailscale.Peer{Name: "exit-a"}}}
	})
	m.RunHealth(context.Background())
	s := m.Snapshot()
	if _, f := s.Health.Counts(); f != 0 {
		t.Fatalf("accepted exit node still counts %d failures", f)
	}
	for _, r := range s.Reasons {
		if r.Code == ReasonTSExitNode || r.Code == ReasonHealth {
			t.Errorf("reason %+v with an accepted exit node", r)
		}
	}
	// Another exit node: the acceptance no longer applies.
	m.update(func(s *Snapshot) { s.Tailscale.State.ExitNode = &tailscale.Peer{Name: "exit-b"} })
	if _, f := m.Snapshot().Health.Counts(); f != 2 {
		t.Errorf("other exit node: %d failures, want 2", f)
	}
	m.SetExitNodeAccepted("exit-b")
	if _, f := m.Snapshot().Health.Counts(); f != 0 {
		t.Errorf("after accepting exit-b: %d failures", f)
	}
}
