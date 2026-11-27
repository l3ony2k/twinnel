package main

import (
	"encoding/json"
	"testing"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

func dnsSnap(phase string, writes bool) status.Snapshot {
	s := status.Snapshot{Loaded: true, WritesAllowed: writes,
		Mihomo: status.Mihomo{Err: "connection refused"},
		Helper: status.Helper{State: status.HelperConnected, Status: &rpc.Status{DNSWatch: &rpc.DNSWatch{Phase: phase}}}}
	status.Evaluate(&s)
	return s
}

func TestDNSRestoredBannerActs(t *testing.T) {
	a := &app{}
	var keys []string
	for _, act := range a.bannerActs(dnsSnap(rpc.DNSWatchRestored, true)) {
		keys = append(keys, act.key)
	}
	want := []string{"dns-restart", "dns-log", "dns-keep", "recovery"}
	if len(keys) != len(want) {
		t.Fatalf("acts %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("acts %v, want %v", keys, want)
		}
	}
	// Read-only: only what needs no helper.
	keys = nil
	for _, act := range a.bannerActs(dnsSnap(rpc.DNSWatchRestored, false)) {
		keys = append(keys, act.key)
	}
	if len(keys) != 2 || keys[0] != "dns-log" {
		t.Fatalf("read-only acts %v", keys)
	}
	// Not restored: the usual mihomo-down actions, none of the DNS ones.
	for _, act := range a.bannerActs(dnsSnap(rpc.DNSWatchWatching, true)) {
		if act.key == "dns-keep" || act.key == "dns-log" {
			t.Fatalf("DNS action while watching: %s", act.key)
		}
	}
	if !dnsOwned(dnsSnap(rpc.DNSWatchWatching, true)) || dnsOwned(dnsSnap(rpc.DNSWatchOff, true)) {
		t.Fatal("dnsOwned")
	}
}

func TestDNSWatchEventLog(t *testing.T) {
	T := i18n.T
	ev := func(w rpc.DNSWatch) eventRow {
		b, _ := json.Marshal(w)
		r, ok := describeEvent(rpc.Event{Type: rpc.EventDNSWatch, Data: b}, nil)
		if !ok {
			t.Fatal("dns.watchdog not described")
		}
		return r
	}
	if r := ev(rpc.DNSWatch{Phase: rpc.DNSWatchRestored}); r.text != T.EvDNSRestored || !r.failed {
		t.Fatalf("restored: %+v", r)
	}
	if r := ev(rpc.DNSWatch{Phase: rpc.DNSWatchWatching}); r.text != T.EvDNSBack || r.failed {
		t.Fatalf("back: %+v", r)
	}
	if r := ev(rpc.DNSWatch{Phase: rpc.DNSWatchWatching, LeftAlone: []string{"Wi-Fi"}}); r.text != i18n.F(T.EvDNSBackLeft, 1) {
		t.Fatalf("left: %+v", r)
	}
	if r := ev(rpc.DNSWatch{Phase: rpc.DNSWatchOff}); r.text != T.EvDNSEnded {
		t.Fatalf("ended: %+v", r)
	}
}
