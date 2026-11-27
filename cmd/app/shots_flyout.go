package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// flyoutShots (DUET_SHOTS_FLOW=flyout) captures the menu bar flyout in
// its main states: everything fine, attention (health warnings and no
// helper), mihomo down, Tailscale logged out. Each state is the live
// (fake) snapshot with a few fields overridden in memory (shotSnap):
// nothing is written anywhere.
func (a *app) flyoutShots(save func(*mygo.Window, string)) {
	report := func(warn, fail int) *health.Report {
		r := &health.Report{Platform: "darwin", Started: time.Now().Add(-2 * time.Minute)}
		add := func(n int, st health.Status) {
			for range n {
				r.Checks = append(r.Checks, health.CheckReport{ID: "c", Result: health.Result{Status: st}})
			}
		}
		add(9, health.StatusOK)
		add(warn, health.StatusWarn)
		add(fail, health.StatusFail)
		return r
	}
	connected := status.Helper{State: status.HelperConnected, Status: &rpc.Status{HelperVersion: "0.1.0"}}
	states := []struct {
		name string
		fn   func(s *status.Snapshot)
	}{
		{"flyout-ok", func(s *status.Snapshot) {
			s.Helper = connected
			s.Health = status.Health{Report: report(0, 0), Updated: time.Now().Add(-2 * time.Minute)}
		}},
		{"flyout-attention", func(s *status.Snapshot) {
			s.WritesAllowed = true // show the helper locks; the app itself stays read-only
			s.Helper = status.Helper{State: status.HelperNotInstalled}
			s.Health = status.Health{Report: report(3, 0), Updated: time.Now().Add(-2 * time.Minute)}
		}},
		{"flyout-mihomo-down", func(s *status.Snapshot) {
			s.Helper = connected
			s.Health = status.Health{Report: report(0, 2), Updated: time.Now().Add(-1 * time.Minute)}
			s.Mihomo = status.Mihomo{Err: "dial tcp 127.0.0.1:9090: connect: connection refused"}
		}},
		{"flyout-ts-login", func(s *status.Snapshot) {
			s.Helper = connected
			s.Health = status.Health{Report: report(0, 0), Updated: time.Now().Add(-2 * time.Minute)}
			s.Tailscale.State.BackendState = "NeedsLogin"
			s.Tailscale.State.ExitNode = nil
		}},
	}
	mygo.RunOnMain(a.togglePanel)
	time.Sleep(1200 * time.Millisecond)
	for _, st := range states {
		mygo.RunOnMain(func() {
			a.shotSnap = func(s *status.Snapshot) {
				st.fn(s)
				s.Loaded = true
				status.Evaluate(s)
			}
			if a.panel != nil {
				a.panel.Invalidate()
			}
		})
		time.Sleep(900 * time.Millisecond)
		var panel *mygo.Window
		mygo.RunOnMain(func() { panel = a.panel })
		if panel != nil {
			save(panel, st.name)
		}
	}
}
