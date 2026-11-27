package status

import (
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// After takeover, failures that only an undecided exit node causes are a
// choice to make (amber), not a breakage (red): design review U4.
func TestExitNodeOnlyFailuresWarn(t *testing.T) {
	check := func(id string, st health.Status, causes ...string) health.CheckReport {
		return health.CheckReport{ID: id, Result: health.Result{Status: st, Causes: causes}}
	}
	snap := func(extra ...health.CheckReport) Snapshot {
		checks := append([]health.CheckReport{
			check("exit-node", health.StatusFail, health.CauseExitNode),
			check("magicdns", health.StatusFail),
			check("tailnet-route", health.StatusFail),
		}, extra...)
		return Snapshot{
			Loaded: true,
			Mihomo: Mihomo{Reachable: true, Version: "v1"},
			Tailscale: Tailscale{Reachable: true, State: tailscale.State{BackendState: "Running",
				ExitNode: &tailscale.Peer{Name: "exit-sg"}}},
			Helper: Helper{State: HelperConnected, Status: &rpc.Status{ActiveSubscription: "main"}},
			Health: Health{Report: &health.Report{Checks: checks}},
		}
	}
	s := snap()
	s.Mihomo.Config.Tun.Enable = true
	Evaluate(&s)
	if s.Level != LevelWarn {
		t.Errorf("exit node only: level %v, want warn (%+v)", s.Level, s.Reasons)
	}
	s = snap(check("dns-hijack", health.StatusFail))
	s.Mihomo.Config.Tun.Enable = true
	Evaluate(&s)
	if s.Level != LevelError {
		t.Errorf("another failure too: level %v, want error", s.Level)
	}
}

// A down service is said in words; the system's error stays in Raw (N3).
func TestDownReasonInWords(t *testing.T) {
	s := Snapshot{Loaded: true, Mihomo: Mihomo{Err: "dial tcp 127.0.0.1:1: connect: connection refused"},
		Tailscale: Tailscale{Reachable: true, State: tailscale.State{BackendState: "Running"}}}
	Evaluate(&s)
	r := s.Reasons[0]
	if strings.Contains(r.Text, "refused") || r.Raw == "" {
		t.Errorf("reason %+v: want words in Text, the error in Raw", r)
	}
}
