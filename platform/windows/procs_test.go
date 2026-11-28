package windows

import (
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/status"
)

func ft(t time.Time) int64 { return t.UnixNano()/100 + filetimeEpoch }

// The service-hosted mihomo's figures: memory (private working set, else
// the working set), uptime, and CPU since the last read (the first: since
// it started). The idle process is left out; a PID reused by a new
// process starts over.
func TestProcessListerEntries(t *testing.T) {
	start := time.Date(2026, 11, 28, 7, 0, 0, 0, time.UTC)
	now := start.Add(time.Hour)
	var l ProcessLister
	s := []procSample{
		{pid: 0, name: ""},
		{pid: 16124, name: "mihomo.exe", private: 40 << 20, ws: 60 << 20, cpu: int64(36 * time.Second / 100), create: ft(start)},
		{pid: 900, name: "tailscaled.exe", ws: 30 << 20, cpu: 0, create: ft(start)},
	}
	got := l.entries(s, now)
	if len(got) != 2 {
		t.Fatalf("entries = %+v", got)
	}
	m := got[0]
	if m.PID != 16124 || m.RSS != 40<<20 || m.Elapsed != time.Hour || m.Command != "mihomo.exe" {
		t.Errorf("mihomo = %+v", m)
	}
	if m.CPU < 0.99 || m.CPU > 1.01 { // 36 s in an hour
		t.Errorf("first CPU = %v, want 1", m.CPU)
	}
	if got[1].RSS != 30<<20 {
		t.Errorf("working set fallback = %d", got[1].RSS)
	}
	// 10 s later, 5 s more CPU: half a core.
	s[1].cpu += int64(5 * time.Second / 100)
	got = l.entries(s, now.Add(10*time.Second))
	if c := got[0].CPU; c < 49.9 || c > 50.1 {
		t.Errorf("CPU since last = %v, want 50", c)
	}
	// The PID is reused by a process that started just now.
	s[1].create, s[1].cpu = ft(now.Add(19*time.Second)), int64(time.Second/100)
	got = l.entries(s, now.Add(20*time.Second))
	if c := got[0].CPU; c < 99 || c > 101 {
		t.Errorf("reused PID CPU = %v, want 100", c)
	}
	if !status.IsMihomoCommand(got[0].Command) || !status.IsTailscaleDaemon(got[1].Command) {
		t.Error("Windows image names not recognized")
	}
}
