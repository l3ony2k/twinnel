package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/engine"
	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/health/fixture"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/store"
)

type allowPeers struct{}

func (allowPeers) Authorize(rpc.Peer) error { return nil }

// syncBuf is a log sink the helper's goroutines write to.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// scratchHelper is an engine with health checks over the synthetic
// fixture machine (no host reads, no network), served on a temp socket,
// and a HelperConn connected to it: the helper side of the 检测出口 path.
type scratchHelper struct {
	e     *engine.Engine
	state string
	log   *syncBuf
	conn  *status.HelperConn
}

func newScratchHelper(t *testing.T) *scratchHelper {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir, err := os.MkdirTemp("", "duetegress") // short: unix socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	st, err := store.Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuf{}
	e := engine.New(st, nil)
	e.Log = slog.New(slog.NewTextHandler(logs, nil))
	env := func(egress bool) health.Env {
		p := health.DefaultSettings()
		p.Egress = egress
		return &fixture.Env{M: fixture.Synthetic(), Prefs: p}
	}
	sched := &health.Scheduler{
		Runner:      &health.Runner{Registry: health.DefaultRegistry(), Timeout: 5 * time.Second},
		NewEnv:      func() health.Env { return env(false) },
		Interval:    time.Hour,
		SkipInitial: true,
	}
	e.SetHealth(sched)
	// As cmd/helper's egressProbe, over the fixture.
	e.EgressProbe = func(ctx context.Context) health.CheckReport {
		r := &health.Runner{Registry: health.DefaultRegistry(), Timeout: 5 * time.Second, Only: []string{health.LeakEgress{}.ID()}}
		return r.Run(ctx, env(true)).Checks[0]
	}
	sched.RunNow(context.Background(), "start")

	sock := filepath.Join(dir, "h.sock")
	tr := rpc.NewUnixTransport()
	l, err := tr.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := rpc.NewServer(tr, allowPeers{}, e.Log)
	e.Register(srv)
	go srv.Serve(l)
	ctx, cancel := context.WithCancel(context.Background())
	conn := &status.HelperConn{Socket: sock}
	go conn.Run(ctx)
	t.Cleanup(func() { cancel(); l.Close(); srv.Close() })
	deadline := time.Now().Add(5 * time.Second)
	for !conn.State().Connected() {
		if time.Now().After(deadline) {
			t.Fatal("the scratch helper did not connect")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return &scratchHelper{e: e, state: filepath.Join(dir, "state"), log: logs, conn: conn}
}

// egressCheck is the egress check of the helper's last report.
func (h *scratchHelper) egressCheck(t *testing.T) (health.CheckReport, *health.Report) {
	t.Helper()
	var hr rpc.HealthReport
	if err := h.conn.Call(context.Background(), rpc.MethodHealthLast, rpc.HealthLastParams{}, &hr); err != nil {
		t.Fatal(err)
	}
	var rep health.Report
	if err := json.Unmarshal(hr.Report, &rep); err != nil {
		t.Fatal(err)
	}
	for _, c := range rep.Checks {
		if c.ID == (health.LeakEgress{}).ID() {
			return c, &rep
		}
	}
	t.Fatal("no egress check in the report")
	return health.CheckReport{}, nil
}

// TestEgressClickReachesHelper: 检测出口 on the row → health.egress on
// the helper → health-egress.json and a log line → the row shows when it
// was tested and what it found. A failure shows a failedRow on the row and
// is logged by the helper.
func TestEgressClickReachesHelper(t *testing.T) {
	defer i18n.Set("zh")
	i18n.Set("zh")
	T := i18n.T
	h := newScratchHelper(t)

	cr, rep := h.egressCheck(t)
	if !egressOff(cr) {
		t.Fatalf("before the test the row should be untested: %+v", cr)
	}
	posted := make(chan func(), 1)
	a := &app{hc: h.conn, healthOpen: map[string]bool{}, onMain: func(f func()) { posted <- f }}
	a.hd.settings = &rpc.Settings{}
	snap := func() status.Snapshot {
		s := status.Snapshot{Time: time.Now(), WritesAllowed: true}
		s.Helper.State = status.HelperConnected
		s.Health.Report = rep
		return s
	}
	row := cr
	tr := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(20, 20).Children(func() { a.healthCheckRow(c.Key("r"), row, snap(), false) })
	}, 900, 300)
	for _, want := range []string{T.EgressUntested, T.FixTestEgress, T.EgressPeriodic} {
		if !tr.HasText(want) {
			t.Errorf("untested row: no %q in %v", want, tr.Texts())
		}
	}
	if tr.HasText("未开启") {
		t.Error("the row still says 未开启")
	}

	if err := tr.Click(T.FixTestEgress); err != nil {
		t.Fatal(err)
	}
	if !a.egress.running {
		t.Fatal("the click did not start the test")
	}
	select {
	case f := <-posted:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("the test did not finish")
	}
	if a.egress.err != "" || a.egress.running {
		t.Fatalf("egress: %+v", a.egress)
	}
	if _, err := os.Stat(filepath.Join(h.state, "health-egress.json")); err != nil {
		t.Errorf("the helper kept no result: %v", err)
	}
	if lg := h.log.String(); !strings.Contains(lg, "method=health.egress") || !strings.Contains(lg, `msg="egress test" status=ok`) {
		t.Errorf("helper log has no egress test:\n%s", lg)
	}
	row, rep = h.egressCheck(t)
	if row.Status != health.StatusOK || row.At.IsZero() {
		t.Fatalf("stored result: %+v", row)
	}
	tr.Frame()
	line := egressLine(row, snap())
	if !strings.HasPrefix(line, row.At.Local().Format("15:04")+" 检测 · ") || !tr.HasText(line) || !tr.HasText(T.EgressRetest) {
		t.Errorf("tested row: want %q and %q in %v", line, T.EgressRetest, tr.Texts())
	}

	// The helper cannot run it: the row says so and the helper logs it.
	h.e.EgressProbe = nil
	if err := tr.Click(T.EgressRetest); err != nil {
		t.Fatal(err)
	}
	(<-posted)()
	if a.egress.err == "" {
		t.Fatal("a failed run left no error")
	}
	tr.Frame()
	if !tr.HasText(T.EgressFailed) || !tr.HasText(T.Retry) {
		t.Errorf("failed run: no failedRow in %v", tr.Texts())
	}
	if lg := h.log.String(); !strings.Contains(lg, `level=WARN msg="egress test"`) {
		t.Errorf("helper log has no failed egress test:\n%s", lg)
	}
}

func TestRunEgressErrors(t *testing.T) {
	ctx := context.Background()
	if _, _, err := runEgress(ctx, nil, nil); err == nil {
		t.Error("no helper and no probe should fail")
	}
	if _, _, err := runEgress(ctx, nil, func(context.Context) *health.CheckReport { return nil }); err == nil {
		t.Error("a probe without a result should fail")
	}
	// An old helper without health.egress: the in-process probe runs.
	old := &fakeHelper{state: status.HelperConnected, noRPC: true}
	cr, local, err := runEgress(ctx, old, func(context.Context) *health.CheckReport {
		return &health.CheckReport{ID: health.LeakEgress{}.ID(), Result: health.Result{Status: health.StatusOK}}
	})
	if err != nil || !local || cr == nil || cr.At.IsZero() {
		t.Errorf("old helper: %+v %v %v", cr, local, err)
	}
}
