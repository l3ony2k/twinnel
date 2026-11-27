package status

import (
	"context"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/mihomo"
)

// delayFake counts per-node delay requests and their concurrency. While
// gate is set, a test waits for it.
type delayFake struct {
	*fakeMihomo
	mu        sync.Mutex
	hits      map[string]int
	running   int
	maxActive int
	gate      chan struct{}
	ms        map[string]int // default 100; -1 = mihomo answers 504
}

func newDelayFake(px map[string]mihomo.Proxy) *delayFake {
	return &delayFake{fakeMihomo: &fakeMihomo{proxies: px, tun: true}, hits: map[string]int{}, ms: map[string]int{}}
}

func (f *delayFake) Delay(ctx context.Context, name, _ string, _ time.Duration) (int, error) {
	f.mu.Lock()
	f.hits[name]++
	f.running++
	f.maxActive = max(f.maxActive, f.running)
	gate := f.gate
	ms, ok := f.ms[name]
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.running--
		f.mu.Unlock()
	}()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	if !ok {
		ms = 100
	}
	if ms < 0 {
		return 0, &mihomo.APIError{Status: 504, Message: "Timeout"}
	}
	return ms, nil
}

func (f *delayFake) GroupDelay(context.Context, string, string, time.Duration) (map[string]int, error) {
	panic("group delay must not be used")
}

func (f *delayFake) counts() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.hits)
}

// overlapping is a synthetic config shaped like the owner's: PROXY holds
// the nodes and REJECT; OpenAI and AI Suite pick PROXY; YouTube nests
// PROXY next to two of the same nodes and DIRECT; an url-test group shares
// nodes too; GLOBAL lists everything.
func overlapping() map[string]mihomo.Proxy {
	px := map[string]mihomo.Proxy{
		"GLOBAL":   {Name: "GLOBAL", Type: "Selector", Now: "DIRECT", All: []string{"PROXY", "OpenAI", "AI Suite", "YouTube", "Auto", "DIRECT", "REJECT"}},
		"PROXY":    {Name: "PROXY", Type: "Selector", Now: "us-1", All: []string{"us-1", "us-2", "sg-1", "jp-1", "REJECT"}},
		"OpenAI":   {Name: "OpenAI", Type: "Selector", Now: "PROXY", All: []string{"PROXY", "us-1", "us-2"}},
		"AI Suite": {Name: "AI Suite", Type: "Selector", Now: "PROXY", All: []string{"PROXY", "OpenAI", "REJECT-DROP"}},
		"YouTube":  {Name: "YouTube", Type: "Selector", Now: "sg-1", All: []string{"PROXY", "us-1", "sg-1", "DIRECT", "PASS"}},
		"Auto":     {Name: "Auto", Type: "URLTest", Now: "us-2", All: []string{"us-1", "us-2", "hk-1"}},
		"Block":    {Name: "Block", Type: "Selector", Now: "REJECT", All: []string{"REJECT", "DIRECT"}},
	}
	for _, n := range []string{"us-1", "us-2", "sg-1", "jp-1", "hk-1"} {
		px[n] = mihomo.Proxy{Name: n, Type: "Vless", Alive: true}
	}
	px["DIRECT"] = mihomo.Proxy{Name: "DIRECT", Type: "Direct", Alive: true}
	px["REJECT"] = mihomo.Proxy{Name: "REJECT", Type: "Reject", Alive: true}
	px["REJECT-DROP"] = mihomo.Proxy{Name: "REJECT-DROP", Type: "RejectDrop", Alive: true}
	px["PASS"] = mihomo.Proxy{Name: "PASS", Type: "Pass", Alive: true}
	return px
}

func startDelayMonitor(t *testing.T, f *delayFake) (*Monitor, context.Context) {
	m, ctx, _ := startDelayMonitorCancel(t, f)
	return m, ctx
}

func startDelayMonitorCancel(t *testing.T, f *delayFake) (*Monitor, context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := NewMonitor(Options{Mihomo: f, MihomoPoll: time.Hour, PrimaryGroup: "PROXY"})
	m.Run(ctx)
	waitFor(t, m, "groups", func(s Snapshot) bool { return len(s.Mihomo.Groups) > 0 })
	return m, ctx, cancel
}

func groupNamed(s Snapshot, name string) Group {
	for _, g := range s.Mihomo.Groups {
		if g.Name == name {
			return g
		}
	}
	return Group{}
}

func TestTestAllHitsEachLeafOnce(t *testing.T) {
	f := newDelayFake(overlapping())
	f.ms["sg-1"] = 382
	f.ms["jp-1"] = -1
	m, ctx := startDelayMonitor(t, f)
	if err := m.TestAll(ctx); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"us-1": 1, "us-2": 1, "sg-1": 1, "jp-1": 1, "hk-1": 1}
	if got := f.counts(); !maps.Equal(got, want) {
		t.Errorf("hits %v, want %v (no built-ins, no groups, each node once)", got, want)
	}
	if d, n, run := m.DelayProgress(); run || d != 5 || n != 5 {
		t.Errorf("progress %d/%d running %v", d, n, run)
	}

	s := m.Snapshot()
	// Group delays come from the resolved node; nested tiles show their
	// node's; built-ins show "—", never 超时.
	for name, want := range map[string]string{"PROXY": "100 ms", "OpenAI": "100 ms", "AI Suite": "100 ms", "YouTube": "382 ms", "Auto": "100 ms", "Block": NoDelay} {
		if got := groupNamed(s, name).DelayText(); got != want {
			t.Errorf("group %s: %q, want %q", name, got, want)
		}
	}
	for _, mem := range groupNamed(s, "PROXY").Members {
		want := map[string]string{"us-1": "100 ms", "us-2": "100 ms", "sg-1": "382 ms", "jp-1": "超时", "REJECT": NoDelay}[mem.Name]
		if mem.DelayText() != want {
			t.Errorf("PROXY/%s: %q, want %q", mem.Name, mem.DelayText(), want)
		}
	}
	yt := groupNamed(s, "YouTube")
	for _, mem := range yt.Members {
		want := map[string]string{"PROXY": "100 ms", "us-1": "100 ms", "sg-1": "382 ms", "DIRECT": NoDelay, "PASS": NoDelay}[mem.Name]
		if mem.DelayText() != want {
			t.Errorf("YouTube/%s: %q, want %q", mem.Name, mem.DelayText(), want)
		}
	}
}

func TestDelayTestsShareInFlightAndBound(t *testing.T) {
	px := overlapping()
	// More nodes than the concurrency bound.
	all := px["PROXY"]
	for i := range 20 {
		n := "x-" + string(rune('a'+i))
		px[n] = mihomo.Proxy{Name: n, Type: "Vless"}
		all.All = append(all.All, n)
	}
	px["PROXY"] = all
	f := newDelayFake(px)
	f.gate = make(chan struct{})
	m, ctx := startDelayMonitor(t, f)

	allDone := make(chan error, 1)
	go func() { allDone <- m.TestAll(ctx) }()
	waitFor(t, m, "batch running", func(Snapshot) bool { _, _, r := m.DelayProgress(); return r })
	if !m.Testing("YouTube") || !m.Testing("OpenAI") || m.Testing("Block") {
		t.Error("Testing: groups whose nodes run should say so; Block has only built-ins")
	}
	// Per-group and per-node tests while the batch runs join it.
	groupDone := make(chan error, 1)
	go func() { groupDone <- m.TestGroup(ctx, "YouTube") }()
	nodeDone := make(chan int, 1)
	go func() { d, _ := m.TestNode(ctx, "sg-1"); nodeDone <- d }()
	if d, err := m.TestNode(ctx, "REJECT"); d != 0 || err != nil {
		t.Errorf("built-in test %d %v", d, err)
	}
	time.Sleep(50 * time.Millisecond)
	close(f.gate)
	if err := <-allDone; err != nil {
		t.Fatal(err)
	}
	if err := <-groupDone; err != nil {
		t.Fatal(err)
	}
	if d := <-nodeDone; d != 100 {
		t.Errorf("joined node test %d", d)
	}
	for n, c := range f.counts() {
		if c != 1 {
			t.Errorf("%s hit %d times", n, c)
		}
	}
	if len(f.counts()) != 25 {
		t.Errorf("%d nodes tested, want 25", len(f.counts()))
	}
	f.mu.Lock()
	peak := f.maxActive
	f.mu.Unlock()
	if peak > DelayConcurrency || peak < 2 {
		t.Errorf("peak concurrency %d, want 2..%d", peak, DelayConcurrency)
	}

	// A group test right after skips the fresh results.
	if err := m.TestGroup(ctx, "PROXY"); err != nil {
		t.Fatal(err)
	}
	if f.counts()["us-1"] != 1 {
		t.Error("fresh node tested again")
	}
	// An explicit node test always tests.
	if _, err := m.TestNode(ctx, "us-1"); err != nil || f.counts()["us-1"] != 2 {
		t.Errorf("node retest: %v %d", err, f.counts()["us-1"])
	}
}

func TestGroupTestOnlyItsLeaves(t *testing.T) {
	f := newDelayFake(overlapping())
	m, ctx := startDelayMonitor(t, f)
	if err := m.TestGroup(ctx, "AI Suite"); err != nil { // AI Suite → PROXY, OpenAI → PROXY
		t.Fatal(err)
	}
	want := map[string]int{"us-1": 1, "us-2": 1, "sg-1": 1, "jp-1": 1}
	if got := f.counts(); !maps.Equal(got, want) {
		t.Errorf("hits %v, want %v", got, want)
	}
	if err := m.TestGroup(ctx, "Block"); err != nil || len(f.counts()) != 4 {
		t.Errorf("built-in group tested: %v %v", err, f.counts())
	}
}

func TestDelayCancel(t *testing.T) {
	f := newDelayFake(overlapping())
	f.gate = make(chan struct{}) // never opens
	m, runCtx, stopApp := startDelayMonitorCancel(t, f)
	// The page stops waiting: TestAll returns, the batch is over.
	ctx, cancel := context.WithTimeout(runCtx, 50*time.Millisecond)
	defer cancel()
	if err := m.TestAll(ctx); err == nil {
		t.Error("TestAll returned nil after its context ended")
	}
	if _, _, run := m.DelayProgress(); run {
		t.Error("batch still marked running")
	}
	if !m.Testing("PROXY") {
		t.Error("the started tests should still run (bound to the monitor)")
	}
	// The app goes away: the tests stop and record nothing.
	stopApp()
	deadline := time.Now().Add(5 * time.Second)
	for m.Testing("PROXY") {
		if time.Now().After(deadline) {
			t.Fatal("tests still running after the monitor stopped")
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.mu.Lock()
	n := len(m.d.results)
	m.mu.Unlock()
	if n != 0 {
		t.Errorf("%d results recorded from cancelled tests", n)
	}
}
