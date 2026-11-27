package engine

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/sysdns"
)

// The watchdog in the helper: mihomo's DNS stops answering after
// dns.point, the system DNS goes back, status.get and an event say so,
// and dns.point / dns.restore end the restore.
func TestDNSWatchdogInEngine(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
	h.e.Now = func() time.Time { return now }
	d := &fakeDNS{svcs: []sysdns.Service{{Name: "Wi-Fi"}, {Name: "Ethernet", Servers: []string{"192.0.2.1"}}}}
	h.e.SystemDNS = d
	var down atomic.Bool
	h.e.DNSProbe = func(context.Context, string) error {
		if down.Load() {
			return errors.New("connection refused")
		}
		return nil
	}
	if w := h.e.DNSWatch(); w == nil || w.Phase != rpc.DNSWatchOff {
		t.Fatalf("before point: %+v", w)
	}
	if _, err := h.e.DNSPoint(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.e.Status(ctx); st.DNSWatch == nil || st.DNSWatch.Phase != rpc.DNSWatchWatching {
		t.Fatalf("after point: %+v", st.DNSWatch)
	}
	_, seq := h.e.Bus.Since(0)
	down.Store(true)
	w := h.e.dnsWatchdog()
	for range 6 { // no service manager: unknown counts as down after 20s
		w.Tick(ctx)
		now = now.Add(5 * time.Second)
	}
	st, _ := h.e.Status(ctx)
	if !st.DNSWatch.Restored() || st.DNSWatch.Cause != rpc.DNSCauseStopped || !slices.Equal(st.DNSWatch.Services, []string{"Wi-Fi", "Ethernet"}) {
		t.Fatalf("not restored: %+v", st.DNSWatch)
	}
	if d.svcs[0].Servers != nil || !slices.Equal(d.svcs[1].Servers, []string{"192.0.2.1"}) {
		t.Fatalf("system DNS: %+v", d.svcs)
	}
	if evs, _ := h.e.Bus.Since(seq); !hasEvent(evs, rpc.EventDNSWatch) {
		t.Fatal("no dns.watchdog event")
	}
	// dns.status shows it too.
	if ds, err := h.e.DNSStatus(ctx); err != nil || !ds.Watch.Restored() || ds.Pointed {
		t.Fatalf("dns.status: %+v %v", ds, err)
	}
	// 保持原设置 is dns.restore: the backup goes, and so does the restore.
	if _, err := h.e.DNSRestore(ctx); err != nil {
		t.Fatal(err)
	}
	if w := h.e.DNSWatch(); w.Phase != rpc.DNSWatchOff {
		t.Fatalf("after keep: %+v", w)
	}
	down.Store(false)
	for range 20 {
		w.Tick(ctx)
		now = now.Add(5 * time.Second)
	}
	if d.svcs[0].Servers != nil {
		t.Fatal("pointed back after the user kept the earlier DNS")
	}
}

func hasEvent(evs []rpc.Event, typ string) bool {
	for _, e := range evs {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// D0 end to end in the helper: Windows, Wi-Fi set to 127.0.0.1 by hand
// before Twinnel, no backup. dns.status calls it pointed but unprotected
// (and so does the health guard); dns.backup records automatic without
// touching the DNS, even while mihomo is down; the watchdog then restores
// Wi-Fi to automatic, never to 127.0.0.1.
func TestDNSBackupProtectsHandPointed(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
	h.e.Now = func() time.Time { return now }
	d := &targetDNS{fakeDNS{svcs: []sysdns.Service{{Name: "Wi-Fi", Servers: []string{"127.0.0.1"}}}}}
	h.e.SystemDNS = d
	var down atomic.Bool
	down.Store(true)
	h.e.DNSProbe = func(context.Context, string) error {
		if down.Load() {
			return errors.New("connection refused")
		}
		return nil
	}
	ds, err := h.e.DNSStatus(ctx)
	if err != nil || !ds.Pointed || !slices.Equal(ds.Unprotected, []string{"Wi-Fi"}) || ds.Watch.Phase != rpc.DNSWatchOff {
		t.Fatalf("before: %+v %v", ds, err)
	}
	if g, err := h.e.HealthDNSGuard(ctx); err != nil || !slices.Equal(g.Unprotected, []string{"Wi-Fi"}) || len(g.Restored) != 0 {
		t.Fatalf("guard: %+v %v", g, err)
	}
	// dns.point refuses while mihomo is down; dns.backup works.
	if _, err := h.e.DNSPoint(ctx); err == nil {
		t.Fatal("pointed while mihomo is down")
	}
	ds, err = h.e.DNSBackup(ctx)
	if err != nil || !slices.Equal(ds.Recorded, []string{"Wi-Fi"}) || len(ds.Unprotected) != 0 || ds.Watch.Phase != rpc.DNSWatchWatching {
		t.Fatalf("backup: %+v %v", ds, err)
	}
	if !slices.Equal(d.svcs[0].Servers, []string{"127.0.0.1"}) {
		t.Fatalf("dns.backup changed the DNS: %+v", d.svcs)
	}
	w := h.e.dnsWatchdog()
	for range 6 {
		w.Tick(ctx)
		now = now.Add(5 * time.Second)
	}
	if d.svcs[0].Servers != nil {
		t.Fatalf("not restored to automatic: %+v", d.svcs)
	}
	if g, _ := h.e.HealthDNSGuard(ctx); !slices.Equal(g.Restored, []string{"Wi-Fi"}) {
		t.Fatalf("guard while restored: %+v", g)
	}
}

// dns.point on a service that already points at mihomo records
// automatic, never the target, as what to restore.
func TestDNSPointRecordsAutomaticForPointed(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d := &targetDNS{fakeDNS{svcs: []sysdns.Service{{Name: "Wi-Fi", Servers: []string{"127.0.0.1"}}}}}
	h.e.SystemDNS = d
	h.e.DNSProbe = func(context.Context, string) error { return nil }
	ds, err := h.e.DNSPoint(ctx)
	if err != nil || !slices.Equal(ds.Recorded, []string{"Wi-Fi"}) || len(ds.Backup) != 1 || ds.Backup[0].Servers != nil {
		t.Fatalf("point: %+v %v", ds, err)
	}
	if !slices.Equal(d.svcs[0].Servers, []string{"127.0.0.1", "::1"}) {
		t.Fatalf("not set to the full target: %+v", d.svcs)
	}
}
