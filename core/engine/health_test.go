package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/health/healthtest"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/rpc/healthsrc"
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
	d, _ := os.MkdirTemp("", "dueth")
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
