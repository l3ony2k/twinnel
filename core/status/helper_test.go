package status

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
)

type denyPeers struct{}

func (denyPeers) Authorize(rpc.Peer) error { return errors.New("denied") }

func helperSock(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "dh") // short: unix socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return filepath.Join(d, "h.sock")
}

// fakeHelper serves status.get and events.wait on sock.
func fakeHelper(t *testing.T, sock string, auth rpc.Authorizer) (*rpc.Bus, func()) {
	t.Helper()
	bus := rpc.NewBus(16)
	tr := rpc.NewUnixTransport()
	s := rpc.NewServer(tr, auth, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rpc.Typed(s, rpc.MethodStatus, func(ctx context.Context, _ rpc.Empty) (rpc.Status, error) {
		return rpc.Status{HelperVersion: "test", PID: 42}, nil
	})
	rpc.Typed(s, rpc.MethodEventsWait, func(ctx context.Context, p rpc.EventsWaitParams) (rpc.EventsWaitResult, error) {
		evs, next := bus.Wait(ctx, p.After, time.Duration(p.TimeoutMS)*time.Millisecond)
		return rpc.EventsWaitResult{Events: evs, Next: next}, nil
	})
	l, err := tr.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(l)
	stop := func() { l.Close(); s.Close() }
	t.Cleanup(stop)
	return bus, stop
}

type recorder struct {
	mu     sync.Mutex
	states []Helper
	events []rpc.Event
	ch     chan struct{}
}

func newRecorder() *recorder { return &recorder{ch: make(chan struct{}, 64)} }

func (r *recorder) onChange(h Helper) {
	r.mu.Lock()
	r.states = append(r.states, h)
	r.mu.Unlock()
	r.ch <- struct{}{}
}

func (r *recorder) onEvents(evs []rpc.Event) {
	r.mu.Lock()
	r.events = append(r.events, evs...)
	r.mu.Unlock()
	r.ch <- struct{}{}
}

// waitFor waits until cond holds.
func (r *recorder) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		r.mu.Lock()
		ok := cond()
		r.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-r.ch:
		case <-deadline:
			t.Fatalf("timed out waiting for %s; states %+v", what, r.states)
		}
	}
}

func (r *recorder) last() Helper { return r.states[len(r.states)-1] }

func newConn(sock string, rec *recorder, installed bool) *HelperConn {
	return &HelperConn{
		Socket:      sock,
		Installed:   func() bool { return installed },
		OnChange:    rec.onChange,
		OnEvents:    rec.onEvents,
		PollTimeout: 200 * time.Millisecond,
		MinRetry:    20 * time.Millisecond,
		MaxRetry:    50 * time.Millisecond,
	}
}

func TestHelperConnNotInstalledThenRunning(t *testing.T) {
	sock := helperSock(t)
	rec := newRecorder()
	hc := newConn(sock, rec, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hc.Run(ctx)

	rec.waitFor(t, "not installed", func() bool { return len(rec.states) > 0 && rec.last().State == HelperNotInstalled })
	if err := hc.Call(ctx, rpc.MethodStatus, nil, nil); !errors.Is(err, ErrHelperNotConnected) {
		t.Fatalf("Call while down = %v", err)
	}

	bus, stop := fakeHelper(t, sock, rpc.AllowAll{})
	hc.Retry()
	rec.waitFor(t, "connected", func() bool { return rec.last().State == HelperConnected })
	if st := hc.State().Status; st == nil || st.HelperVersion != "test" {
		t.Fatalf("status = %+v", st)
	}
	var st rpc.Status
	if err := hc.Call(ctx, rpc.MethodStatus, nil, &st); err != nil || st.PID != 42 {
		t.Fatalf("Call = %v, %+v", err, st)
	}

	bus.Publish(rpc.EventStateChanged, map[string]string{"layer": "x"})
	rec.waitFor(t, "event", func() bool { return len(rec.events) == 1 && rec.events[0].Type == rpc.EventStateChanged })

	stop()
	os.Remove(sock)
	rec.waitFor(t, "not running after stop", func() bool {
		s := rec.last().State
		return s == HelperNotRunning || s == HelperNotInstalled
	})
}

func TestHelperConnSkipsOldEvents(t *testing.T) {
	sock := helperSock(t)
	bus, _ := fakeHelper(t, sock, rpc.AllowAll{})
	bus.Publish(rpc.EventHelperStarted, nil) // before the app connected
	rec := newRecorder()
	hc := newConn(sock, rec, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hc.Run(ctx)
	rec.waitFor(t, "connected", func() bool { return len(rec.states) > 0 && rec.last().State == HelperConnected })
	time.Sleep(300 * time.Millisecond) // one poll timeout
	bus.Publish(rpc.EventKernelChanged, nil)
	rec.waitFor(t, "new event", func() bool { return len(rec.events) > 0 })
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, e := range rec.events {
		if e.Type == rpc.EventHelperStarted {
			t.Fatalf("got an event from before connecting: %+v", rec.events)
		}
	}
}

func TestHelperConnStates(t *testing.T) {
	t.Run("installed but no socket", func(t *testing.T) {
		rec := newRecorder()
		hc := newConn(helperSock(t), rec, true)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hc.Run(ctx)
		rec.waitFor(t, "not running", func() bool { return len(rec.states) > 0 && rec.last().State == HelperNotRunning })
	})
	t.Run("stale socket, nothing installed", func(t *testing.T) {
		// A socket file nobody listens on answers "connection refused":
		// still 未安装 when the installed files are missing.
		sock := helperSock(t)
		l, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		l.(*net.UnixListener).SetUnlinkOnClose(false)
		l.Close()
		rec := newRecorder()
		hc := newConn(sock, rec, false)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hc.Run(ctx)
		rec.waitFor(t, "not installed", func() bool { return len(rec.states) > 0 && rec.last().State == HelperNotInstalled })
	})
	t.Run("denied", func(t *testing.T) {
		sock := helperSock(t)
		fakeHelper(t, sock, denyPeers{})
		rec := newRecorder()
		hc := newConn(sock, rec, true)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hc.Run(ctx)
		rec.waitFor(t, "denied", func() bool { return len(rec.states) > 0 && rec.last().State == HelperDenied })
	})
}

func TestMonitorSetHelper(t *testing.T) {
	m := NewMonitor(Options{Mihomo: &fakeMihomoForHelper{}})
	lvl := m.Snapshot().Level
	m.SetHelper(Helper{State: HelperNotInstalled})
	s := m.Snapshot()
	if s.Helper.State != HelperNotInstalled || s.Level != lvl {
		t.Fatalf("helper %v level %v (was %v)", s.Helper.State, s.Level, lvl)
	}
}

// fakeMihomoForHelper is never called: the monitor is not run.
type fakeMihomoForHelper struct{ MihomoSource }
