package engine

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/health/healthtest"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/rpc/healthsrc"
)

// stubEnv is a health.Env whose only fact is the platform; the fake check
// below asks for nothing else.
type stubEnv struct{ health.Env }

func (stubEnv) Platform() string { return health.Darwin }

type switchCheck struct {
	mu     sync.Mutex
	status health.Status
	runs   int
}

func (c *switchCheck) ID() string          { return "fake" }
func (c *switchCheck) Title() string       { return "Fake" }
func (c *switchCheck) Platforms() []string { return []string{health.Darwin} }
func (c *switchCheck) set(s health.Status) { c.mu.Lock(); c.status = s; c.mu.Unlock() }
func (c *switchCheck) count() int          { c.mu.Lock(); defer c.mu.Unlock(); return c.runs }
func (c *switchCheck) Run(context.Context, health.Env) health.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runs++
	return health.Result{Status: c.status, Summary: string(c.status)}
}

func newHealth(h *harness) (*switchCheck, *health.Scheduler) {
	chk := &switchCheck{status: health.StatusOK}
	reg := health.NewRegistry()
	reg.Register(chk)
	s := &health.Scheduler{
		Runner: &health.Runner{Registry: reg}, Env: stubEnv{},
		Interval: time.Hour, Debounce: 10 * time.Millisecond, SkipInitial: true,
	}
	h.e.SetHealth(s)
	return chk, s
}

func eventTypes(b *rpc.Bus) map[string][]json.RawMessage {
	evs, _ := b.Wait(context.Background(), 0, time.Millisecond)
	out := map[string][]json.RawMessage{}
	for _, e := range evs {
		out[e.Type] = append(out[e.Type], e.Data)
	}
	return out
}

func TestHealthRPC(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := h.e.HealthLast(""); err == nil {
		t.Error("health.last without a scheduler should fail")
	}
	chk, _ := newHealth(h)

	// Serve the engine over a real socket and read it with healthsrc.
	d, _ := os.MkdirTemp("", "twinnelh")
	t.Cleanup(func() { os.RemoveAll(d) })
	sock := filepath.Join(d, "h.sock")
	tr := rpc.NewUnixTransport()
	l, err := tr.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := rpc.NewServer(tr, allowAll{}, h.e.Log)
	h.e.Register(srv)
	go srv.Serve(l)
	t.Cleanup(func() { l.Close(); srv.Close() })
	c, err := rpc.Dial(ctx, nil, sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	src := healthsrc.Source{Client: c}
	// No report yet: Run falls back to health.run.
	rep, err := src.Run(ctx)
	if err != nil || rep == nil || rep.Overall != health.StatusOK || chk.count() != 1 {
		t.Fatalf("first run: %+v %v (runs %d)", rep, err, chk.count())
	}
	// Now Run reads the stored report.
	if rep, err = src.Run(ctx); err != nil || rep == nil || chk.count() != 1 {
		t.Fatalf("last: %+v %v (runs %d)", rep, err, chk.count())
	}
	chk.set(health.StatusFail)
	if rep, err = (healthsrc.Source{Client: c, Fresh: true}).Run(ctx); err != nil || rep.Overall != health.StatusFail {
		t.Fatalf("fresh: %+v %v", rep, err)
	}
	var last rpc.HealthReport
	if err := c.Call(ctx, rpc.MethodHealthLast, nil, &last); err != nil || last.Reason != "app" || last.At.IsZero() {
		t.Errorf("health.last: %+v %v", last, err)
	}

	ev := eventTypes(h.e.Bus)
	if len(ev[rpc.EventHealthUpdated]) != 2 || len(ev[rpc.EventCheckChanged]) != 1 {
		t.Fatalf("events: %v", ev)
	}
	ch, err := healthsrc.DecodeChange(rpc.Event{Data: ev[rpc.EventCheckChanged][0]})
	if err != nil || ch.ID != "fake" || ch.From != health.StatusOK || ch.To != health.StatusFail {
		t.Errorf("check.changed: %+v %v", ch, err)
	}
}

func TestHealthTriggers(t *testing.T) {
	h := newHarness(t)
	chk, _ := newHealth(h)
	net := make(chan string, 1)
	h.e.NetChanges = net
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { h.e.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	h.apply("triggers a health run")
	healthtest.WaitUntil(t, "health run after apply", func() bool { return chk.count() >= 1 })
	n := chk.count()
	net <- "network: route"
	healthtest.WaitUntil(t, "health run after network change", func() bool { return chk.count() > n })
	if h.e.healthSched().Last() == nil {
		t.Error("no report kept")
	}
}

type allowAll struct{}

func (allowAll) Authorize(rpc.Peer) error { return nil }

// egressOff is health.LeakEgress as a scheduled run sees it with 定期检测
// off: skipped, the test offered.
type egressOff struct{}

func (egressOff) ID() string          { return health.LeakEgress{}.ID() }
func (egressOff) Title() string       { return "egress" }
func (egressOff) Platforms() []string { return []string{health.Darwin} }
func (egressOff) Run(context.Context, health.Env) health.Result {
	return health.Result{Status: health.StatusSkip, Summary: "未开启"}
}

// 检测出口: health.egress runs the test now, the report shows its result
// with its time, later scheduled runs keep it (until EgressKeep), a new
// helper run reads it back, and 定期检测 on leaves the run's own result.
func TestHealthEgressKeepsResult(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	reg := health.NewRegistry()
	reg.Register(egressOff{})
	newSched := func(e *Engine) *health.Scheduler {
		s := &health.Scheduler{Runner: &health.Runner{Registry: reg}, Env: stubEnv{}, Interval: time.Hour, SkipInitial: true}
		e.SetHealth(s)
		return s
	}
	s := newSched(h.e)
	if _, err := h.e.HealthEgress(ctx, ""); err == nil {
		t.Fatal("health.egress without a probe should fail")
	}
	probes := 0
	h.e.EgressProbe = func(context.Context) health.CheckReport {
		probes++
		return health.CheckReport{ID: health.LeakEgress{}.ID(), Title: "egress", Result: health.Result{Status: health.StatusWarn, Summary: "same IP"}}
	}
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	h.e.Now = func() time.Time { return now }
	s.RunNow(ctx, "start")

	hr, err := h.e.HealthEgress(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	egress := func(raw json.RawMessage) health.CheckReport {
		t.Helper()
		var rep health.Report
		if err := json.Unmarshal(raw, &rep); err != nil {
			t.Fatal(err)
		}
		return rep.Checks[0]
	}
	if c := egress(hr.Report); probes != 1 || c.Status != health.StatusWarn || !c.At.Equal(now) {
		t.Fatalf("health.egress: %d probes, check %+v", probes, c)
	}
	if lr, _ := h.e.HealthLast(""); egress(lr.Report).Status != health.StatusWarn {
		t.Fatal("health.last lost the result")
	}

	// A scheduled run with the switch off keeps it.
	if c := s.RunNow(ctx, "interval").Checks[0]; c.Status != health.StatusWarn || !c.At.Equal(now) {
		t.Fatalf("periodic run: %+v", c)
	}
	// A restarted helper reads it back.
	again := New(h.e.Store, h.e.Secrets)
	again.Now = h.e.Now
	if c := newSched(again).RunNow(ctx, "start").Checks[0]; c.Status != health.StatusWarn {
		t.Fatalf("after restart: %+v", c)
	}
	// Stale: 未开启 again.
	now = now.Add(EgressKeep + time.Minute)
	if c := s.RunNow(ctx, "interval").Checks[0]; c.Status != health.StatusSkip || !c.At.IsZero() {
		t.Fatalf("stale result kept: %+v", c)
	}
	// 定期检测 on: the run's own result stands.
	now = now.Add(-EgressKeep)
	on := true
	if _, err := h.e.UpdateSettings(rpc.SettingsUpdate{HealthEgress: &on}); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.e.Settings(); !st.HealthEgress {
		t.Fatal("health_egress not saved")
	}
	if c := s.RunNow(ctx, "interval").Checks[0]; !c.At.IsZero() {
		t.Fatalf("switch on, the stored result replaced the run's: %+v", c)
	}
}

// The egress test's unmasked addresses reach only a caller that asks
// (the app); the report and its file keep them out, and a restarted
// helper still has them for the stored result.
func TestHealthEgressIPs(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	reg := health.NewRegistry()
	reg.Register(egressOff{})
	newSched := func(e *Engine) *health.Scheduler {
		s := &health.Scheduler{Runner: &health.Runner{Registry: reg}, Env: stubEnv{}, Interval: time.Hour, SkipInitial: true}
		e.SetHealth(s)
		return s
	}
	s := newSched(h.e)
	const via, direct = "203.0.113.50", "198.51.100.7"
	h.e.EgressProbe = func(context.Context) health.CheckReport {
		return health.CheckReport{ID: health.LeakEgress{}.ID(), Title: "egress", Result: health.Result{
			Status: health.StatusOK, Summary: "differs",
			Egress: &health.EgressIPs{Proxied: netip.MustParseAddr(via), Direct: netip.MustParseAddr(direct), Node: "node-a"},
		}}
	}
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	h.e.Now = func() time.Time { return now }
	s.RunNow(ctx, "start")

	hr, err := h.e.withEgressIPs(h.e.HealthEgress(ctx, ""))(true)
	if err != nil {
		t.Fatal(err)
	}
	if hr.Egress == nil || hr.Egress.Proxied != via || hr.Egress.Direct != direct || hr.Egress.Node != "node-a" {
		t.Fatalf("asked: %+v", hr.Egress)
	}
	if strings.Contains(string(hr.Report), via) {
		t.Errorf("unmasked address in the report: %s", hr.Report)
	}
	if hr, _ := h.e.withEgressIPs(h.e.HealthLast(""))(false); hr.Egress != nil {
		t.Errorf("not asked, got %+v", hr.Egress)
	}
	b, err := os.ReadFile(filepath.Join(h.e.Store.Dir, egressFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), via) {
		t.Errorf("unmasked address in %s", egressFile)
	}

	// A restarted helper: the stored result comes back with its addresses.
	again := New(h.e.Store, h.e.Secrets)
	again.Now = h.e.Now
	newSched(again).RunNow(ctx, "start")
	hr, _ = again.withEgressIPs(again.HealthLast(""))(true)
	if hr.Egress == nil || hr.Egress.Proxied != via {
		t.Fatalf("after restart: %+v", hr.Egress)
	}
}
