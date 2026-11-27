package status

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/mihomo"
)

// A /proxies answer as mihomo sends it: the node's history carries the
// delay of the exit, and its time is the result's age.
func TestExitDelayFromHistory(t *testing.T) {
	const body = `{"proxies":{
	 "PROXY":{"name":"PROXY","type":"Selector","now":"Auto","all":["Auto","node-a"],"history":[]},
	 "Auto":{"name":"Auto","type":"URLTest","now":"node-b","all":["node-a","node-b"],"history":[]},
	 "node-a":{"name":"node-a","type":"Vless","alive":true,"history":[]},
	 "node-b":{"name":"node-b","type":"Trojan","alive":true,"history":[
	   {"time":"2026-11-27T10:00:00.123456+08:00","delay":120},
	   {"time":"2026-11-27T10:05:00.5+08:00","delay":31}]}}}`
	var out struct {
		Proxies map[string]mihomo.Proxy `json:"proxies"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	gs := buildGroups(out.Proxies, nil)
	p := PickPrimary(gs, "PROXY")
	if p == nil || p.Node != "node-b" || len(p.Chain) != 2 {
		t.Fatalf("primary %+v: want PROXY → Auto → node-b", p)
	}
	want := time.Date(2026, 11, 27, 2, 5, 0, 5e8, time.UTC)
	if !p.Tested || p.Delay != 31 || !p.DelayAt.Equal(want) {
		t.Errorf("delay %d tested %v at %v; want 31 ms at %v", p.Delay, p.Tested, p.DelayAt, want)
	}

	// The monitor's own newer test wins, with its time; an older one not.
	later := want.Add(time.Minute)
	p = PickPrimary(buildGroups(out.Proxies, map[string]delayResult{"node-b": {ms: 44, at: later}}), "PROXY")
	if p.Delay != 44 || !p.DelayAt.Equal(later) {
		t.Errorf("newer own result: %d at %v", p.Delay, p.DelayAt)
	}
	p = PickPrimary(buildGroups(out.Proxies, map[string]delayResult{"node-b": {ms: 44, at: want.Add(-time.Hour)}}), "PROXY")
	if p.Delay != 31 || !p.DelayAt.Equal(want) {
		t.Errorf("older own result won: %d at %v", p.Delay, p.DelayAt)
	}

	// Untested node: no time.
	out.Proxies["Auto"] = mihomo.Proxy{Name: "Auto", Type: "URLTest", Now: "node-a", All: []string{"node-a", "node-b"}}
	p = PickPrimary(buildGroups(out.Proxies, nil), "PROXY")
	if p.Node != "node-a" || p.Tested || !p.DelayAt.IsZero() {
		t.Errorf("untested node: %+v", p)
	}
}

func TestExitTestDue(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	node := func(tested bool, at time.Time) *Group {
		return &Group{Name: "PROXY", Node: "n", Tested: tested, DelayAt: at}
	}
	for _, tc := range []struct {
		name    string
		g       *Group
		lastTry time.Time
		want    bool
	}{
		{"no primary", nil, time.Time{}, false},
		{"no node", &Group{Name: "PROXY"}, time.Time{}, false},
		{"built-in", &Group{Name: "PROXY", Node: "DIRECT", Builtin: true}, time.Time{}, false},
		{"untested", node(false, time.Time{}), time.Time{}, true},
		{"fresh", node(true, ago(time.Minute)), time.Time{}, false},
		{"stale", node(true, ago(6*time.Minute)), time.Time{}, true},
		{"stale, tried just now", node(true, ago(6*time.Minute)), ago(time.Minute), false},
		{"untested, tried just now (it failed)", node(false, time.Time{}), ago(time.Minute), false},
		{"untested, tried long ago", node(false, time.Time{}), ago(ExitFresh), true},
	} {
		if got := exitTestDue(now, tc.g, tc.lastTry); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestExit tests the primary's node once, then not again until the
// result is ExitFresh old; it never tests other nodes.
func TestTestExitThrottles(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }

	f := newDelayFake(overlapping())
	f.ms["us-1"] = 31
	m := NewMonitor(Options{Mihomo: f, MihomoPoll: time.Hour, PrimaryGroup: "PROXY", Now: clock})
	ctx := t.Context()
	m.Run(ctx)
	waitFor(t, m, "groups", func(s Snapshot) bool { return len(s.Mihomo.Groups) > 0 })

	if !m.TestExit() {
		t.Fatal("untested exit: no test")
	}
	waitFor(t, m, "result", func(s Snapshot) bool { p := s.Mihomo.Primary; return p != nil && p.Tested })
	p := m.Snapshot().Mihomo.Primary
	if p.Delay != 31 || !p.DelayAt.Equal(clock()) {
		t.Errorf("exit %d at %v", p.Delay, p.DelayAt)
	}
	if m.TestExit() {
		t.Error("fresh result tested again")
	}
	advance(ExitFresh - time.Second)
	if m.TestExit() {
		t.Error("tested before ExitFresh")
	}
	advance(2 * time.Second)
	if !m.TestExit() {
		t.Error("stale result not tested")
	}
	waitFor(t, m, "second result", func(Snapshot) bool { return f.counts()["us-1"] == 2 })
	if c := f.counts(); len(c) != 1 {
		t.Errorf("tested %v; only the exit's node may be", c)
	}
}
