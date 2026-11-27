package main

import (
	"net/netip"
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
)

func exitSnap(p *status.Group) status.Snapshot {
	now := time.Date(2026, 11, 27, 21, 40, 0, 0, time.Local)
	return status.Snapshot{Time: now, Mihomo: status.Mihomo{Reachable: true, Primary: p}}
}

func exitGroup() *status.Group {
	return &status.Group{Name: "PROXY", Type: "Selector", Now: "Auto", Chain: []string{"Auto", "🇸🇬 node-a"}, Node: "🇸🇬 node-a"}
}

func egressResult(st health.Status, ips *health.EgressIPs) *health.CheckReport {
	return &health.CheckReport{ID: health.LeakEgress{}.ID(), Result: health.Result{Status: st, Summary: "s", Egress: ips}}
}

func TestExitTileStates(t *testing.T) {
	T := i18n.T
	ip := func(s string) netip.Addr { return netip.MustParseAddr(s) }

	// No data at all: 未测 with 测速 offered, and 检测出口 IP.
	g := exitGroup()
	s := exitSnap(g)
	v := exitTileView(s, false, nil, time.Time{}, true, false, "")
	if v.figure != T.DelayUnknown || !v.offer || v.known || v.age != "" || v.egress != egressNone {
		t.Errorf("no data: %+v", v)
	}
	if v.node != "🇸🇬 node-a" || v.route != "PROXY → Auto → 🇸🇬 node-a" {
		t.Errorf("node %q route %q", v.node, v.route)
	}
	// Testing: no 测速 button, a spinner.
	if v := exitTileView(s, true, nil, time.Time{}, true, false, ""); v.offer || !v.testing {
		t.Errorf("testing: %+v", v)
	}

	// mihomo's history only: the figure and its age.
	g.Tested, g.Delay, g.DelayAt = true, 31, s.Time.Add(-5*time.Minute)
	v = exitTileView(s, false, nil, time.Time{}, true, false, "")
	if v.figure != "31 ms" || !v.known || v.offer || v.age != status.Ago(s.Time, g.DelayAt) {
		t.Errorf("history: %+v", v)
	}
	// Fresh.
	g.DelayAt = s.Time.Add(-3 * time.Second)
	if v := exitTileView(s, false, nil, time.Time{}, true, false, ""); v.age != T.JustNow {
		t.Errorf("fresh age %q", v.age)
	}
	// A timeout is a result too.
	g.Delay = 0
	if v := exitTileView(s, false, nil, time.Time{}, true, false, ""); v.figure != T.DelayTimeout || !v.known {
		t.Errorf("timeout: %+v", v)
	}
	g.Delay = 31

	// Egress known: the IP in full, its time, the direct one for the tooltip.
	at := s.Time.Add(-8 * time.Minute)
	eg := egressResult(health.StatusOK, &health.EgressIPs{Proxied: ip("203.0.113.50"), Direct: ip("198.51.100.7"), Node: g.Node})
	v = exitTileView(s, false, eg, at, true, false, "")
	if v.egress != egressKnown || v.ip != "203.0.113.50" || v.ipDirect != "198.51.100.7" || v.ipAt != at.Format("15:04") {
		t.Errorf("egress known: %+v", v)
	}
	// Measured through another node: not this exit's IP; offer the test.
	eg.Egress.Node = "other"
	if v := exitTileView(s, false, eg, at, true, false, ""); v.egress != egressNone || v.ip != "" {
		t.Errorf("other node: %+v", v)
	}
	// Running.
	if v := exitTileView(s, false, eg, at, true, true, ""); v.egress != egressRunning {
		t.Errorf("running: %+v", v)
	}
	// Failed run: the reason.
	if v := exitTileView(s, false, nil, time.Time{}, true, false, "helper gone"); v.egress != egressFailed || v.why != "helper gone" {
		t.Errorf("failed run: %+v", v)
	}
	// A result that measured nothing (proxied request failed).
	if v := exitTileView(s, false, egressResult(health.StatusWarn, nil), at, true, false, ""); v.egress != egressFailed || v.why != "s" {
		t.Errorf("failed result: %+v", v)
	}
	// The test can't run here (no helper, no probe): no line.
	if v := exitTileView(s, false, nil, time.Time{}, false, false, ""); v.egress != egressNoTest {
		t.Errorf("no test: %+v", v)
	}

	// Built-in exit (DIRECT): no delay, no 测速.
	d := &status.Group{Name: "PROXY", Node: "DIRECT", Chain: []string{"DIRECT"}, Builtin: true}
	if v := exitTileView(exitSnap(d), false, nil, time.Time{}, true, false, ""); v.figure != status.NoDelay || v.offer {
		t.Errorf("direct: %+v", v)
	}
	// mihomo down.
	if v := exitTileView(status.Snapshot{}, false, nil, time.Time{}, true, false, ""); v.node != T.Unreachable || v.egress != egressNoTest {
		t.Errorf("down: %+v", v)
	}
}

// egressNow: the in-process result first, else the report's check unless
// it only says the test is off; a periodic result takes the report's time.
func TestEgressNow(t *testing.T) {
	a := &app{}
	started := time.Date(2026, 11, 27, 21, 0, 0, 0, time.UTC)
	off := health.CheckReport{ID: health.LeakEgress{}.ID(), Result: health.Result{Status: health.StatusSkip,
		Fix: &health.FixHint{AutoFix: health.FixEgressProbe}}}
	s := status.Snapshot{}
	s.Health.Report = &health.Report{Started: started, Checks: []health.CheckReport{off}}
	if cr, _ := a.egressNow(s); cr != nil {
		t.Errorf("off: %+v", cr)
	}
	s.Health.Report.Checks[0] = *egressResult(health.StatusOK, nil)
	if cr, at := a.egressNow(s); cr == nil || !at.Equal(started) {
		t.Errorf("periodic: %+v %v", cr, at)
	}
	local := egressResult(health.StatusOK, nil)
	local.At = started.Add(time.Hour)
	a.egress.result = local
	if cr, at := a.egressNow(s); cr != local || !at.Equal(local.At) {
		t.Errorf("local: %+v %v", cr, at)
	}
}
