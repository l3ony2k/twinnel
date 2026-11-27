package status

import (
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
)

func withDNSWatch(s Snapshot, phase string) Snapshot {
	s.Mihomo.Config.Tun.Enable = true
	s.Helper = Helper{State: HelperConnected, Status: &rpc.Status{DNSWatch: &rpc.DNSWatch{Phase: phase}}}
	return s
}

func TestDNSRestoredReason(t *testing.T) {
	now := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	s := withDNSWatch(base(now), rpc.DNSWatchRestored)
	Evaluate(&s)
	if s.Level != LevelWarn || len(s.Reasons) != 1 || s.Reasons[0].Code != ReasonDNSRestored || s.Reasons[0].Text != i18n.Cur().ReasonDNSRestored {
		t.Fatalf("restored: %v %+v", s.Level, s.Reasons)
	}
	for _, p := range []string{rpc.DNSWatchWatching, rpc.DNSWatchOff} {
		s := withDNSWatch(base(now), p)
		Evaluate(&s)
		if s.Level != LevelOK || len(s.Reasons) != 0 {
			t.Fatalf("%s: %+v", p, s.Reasons)
		}
	}
	// A helper without a watchdog.
	s = base(now)
	s.Helper = Helper{State: HelperConnected, Status: &rpc.Status{}}
	if s.DNSRestored() {
		t.Fatal("no watchdog reads as restored")
	}
}

func TestDNSRestoredAlert(t *testing.T) {
	t0 := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	a := &Alerter{}
	sysFail := func(s Snapshot, at time.Time) Snapshot {
		s.Health.Report = &health.Report{Started: at, Checks: []health.CheckReport{
			{ID: systemDNSCheck, Title: "系统 DNS", Result: health.Result{Status: health.StatusFail}}}}
		return s
	}
	ok := base(t0)
	ok.Health.Report = &health.Report{Started: t0, Checks: []health.CheckReport{
		{ID: systemDNSCheck, Result: health.Result{Status: health.StatusOK}}}}
	a.Check(withDNSWatch(ok, rpc.DNSWatchWatching))
	got := a.Check(sysFail(withDNSWatch(base(t0.Add(time.Minute)), rpc.DNSWatchRestored), t0.Add(time.Minute)))
	if keys(got) != "dns-restored" || got[0].Title != i18n.Cur().AlertDNSRestored {
		t.Fatalf("alerts: %+v", got)
	}
	if got := a.Check(sysFail(withDNSWatch(base(t0.Add(2*time.Minute)), rpc.DNSWatchRestored), t0.Add(2*time.Minute))); len(got) != 0 {
		t.Fatalf("again: %+v", got)
	}
}
