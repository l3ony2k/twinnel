package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// shortSock returns a socket path short enough for sun_path (104 bytes on
// macOS; t.TempDir() is often longer).
func shortSock(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "duetrpc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return filepath.Join(d, "h.sock")
}

type denyAll struct{}

func (denyAll) Authorize(p Peer) error { return errors.New("denied") }

func startServer(t *testing.T, auth Authorizer, setup func(s *Server)) (string, *Server) {
	t.Helper()
	sock := shortSock(t)
	tr := NewUnixTransport()
	s := NewServer(tr, auth, slog.New(slog.NewTextHandler(io.Discard, nil)))
	setup(s)
	l, err := tr.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(l)
	t.Cleanup(func() { l.Close(); s.Close() })
	return sock, s
}

func dial(t *testing.T, sock string) *Client {
	t.Helper()
	c, err := Dial(context.Background(), nil, sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestCallRoundTrip(t *testing.T) {
	var gotPeer Peer
	sock, _ := startServer(t, AllowAll{}, func(s *Server) {
		Typed(s, MethodLayerGet, func(ctx context.Context, p IDParams) (LayerContent, error) {
			if p.ID == "missing" {
				return LayerContent{}, ErrNotFound("layer", p.ID)
			}
			if p.ID == "boom" {
				return LayerContent{}, errors.New("boom")
			}
			return LayerContent{Layer: Layer{ID: p.ID}, Content: "x: 1\n"}, nil
		})
		s.Handle(MethodStatus, func(ctx context.Context, peer Peer, _ json.RawMessage) (any, error) {
			gotPeer = peer
			return Status{UID: peer.UID}, nil
		})
	})
	c := dial(t, sock)
	ctx := context.Background()

	var lc LayerContent
	if err := c.Call(ctx, MethodLayerGet, IDParams{ID: "a"}, &lc); err != nil {
		t.Fatal(err)
	}
	if lc.ID != "a" || lc.Content != "x: 1\n" {
		t.Errorf("got %+v", lc)
	}

	var re *Error
	if err := c.Call(ctx, MethodLayerGet, IDParams{ID: "missing"}, nil); !errors.As(err, &re) || re.Code != CodeNotFound {
		t.Errorf("missing: %v", err)
	}
	if err := c.Call(ctx, MethodLayerGet, IDParams{ID: "boom"}, nil); !errors.As(err, &re) || re.Code != CodeFailed {
		t.Errorf("boom: %v", err)
	}
	if err := c.Call(ctx, MethodLayerGet, map[string]any{"id": "a", "path": "/etc"}, nil); !errors.As(err, &re) || re.Code != CodeInvalidParams {
		t.Errorf("unknown field: %v", err)
	}
	if err := c.Call(ctx, "exec.shell", nil, nil); !errors.As(err, &re) || re.Code != CodeMethodNotFound {
		t.Errorf("unknown method: %v", err)
	}
	if err := c.Call(ctx, MethodKernelList, nil, nil); !errors.As(err, &re) || re.Code != CodeMethodNotFound {
		t.Errorf("unregistered method: %v", err)
	}

	var st Status
	if err := c.Call(ctx, MethodStatus, nil, &st); err != nil {
		t.Fatal(err)
	}
	if gotPeer.UID != os.Getuid() || st.UID != os.Getuid() {
		t.Errorf("peer uid = %d, want %d", gotPeer.UID, os.Getuid())
	}
	if gotPeer.PID != os.Getpid() {
		t.Errorf("peer pid = %d, want %d", gotPeer.PID, os.Getpid())
	}
}

func TestConcurrentCalls(t *testing.T) {
	sock, _ := startServer(t, AllowAll{}, func(s *Server) {
		Typed(s, MethodLayerGet, func(ctx context.Context, p IDParams) (IDParams, error) {
			if p.ID == "slow" {
				time.Sleep(100 * time.Millisecond)
			}
			return p, nil
		})
	})
	c := dial(t, sock)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('a' + i))
			if i == 0 {
				id = "slow"
			}
			var out IDParams
			if err := c.Call(context.Background(), MethodLayerGet, IDParams{ID: id}, &out); err != nil {
				errs <- err
			} else if out.ID != id {
				errs <- errors.New("mismatched response " + out.ID + " for " + id)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestUnauthorized(t *testing.T) {
	sock, _ := startServer(t, denyAll{}, func(s *Server) {
		Typed(s, MethodStatus, func(ctx context.Context, _ Empty) (Status, error) { return Status{}, nil })
	})
	c := dial(t, sock)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.Call(ctx, MethodStatus, nil, nil)
	var re *Error
	if !errors.As(err, &re) || re.Code != CodeUnauthorized {
		t.Fatalf("err = %v, want unauthorized", err)
	}
}

func TestEventsLongPoll(t *testing.T) {
	bus := NewBus(4)
	sock, _ := startServer(t, AllowAll{}, func(s *Server) {
		Typed(s, MethodEventsWait, func(ctx context.Context, p EventsWaitParams) (EventsWaitResult, error) {
			evs, next := bus.Wait(ctx, p.After, time.Duration(p.TimeoutMS)*time.Millisecond)
			return EventsWaitResult{Events: evs, Next: next}, nil
		})
	})
	c := dial(t, sock)

	// Times out with nothing.
	r, err := c.WaitEvents(context.Background(), 0, 50*time.Millisecond)
	if err != nil || len(r.Events) != 0 || r.Next != 0 {
		t.Fatalf("empty wait: %+v %v", r, err)
	}
	// Wakes up on publish.
	go func() {
		time.Sleep(50 * time.Millisecond)
		bus.Publish(EventComposeApplied, map[string]string{"hash": "abc"})
	}()
	start := time.Now()
	r, err = c.WaitEvents(context.Background(), 0, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second || len(r.Events) != 1 || r.Events[0].Type != EventComposeApplied || r.Next != 1 {
		t.Fatalf("wait: %+v", r)
	}
	if string(r.Events[0].Data) != `{"hash":"abc"}` {
		t.Errorf("data = %s", r.Events[0].Data)
	}
	// Overflow the ring: the client sees a truncation marker.
	for range 6 {
		bus.Publish(EventStateChanged, nil)
	}
	r, _ = c.WaitEvents(context.Background(), 1, time.Second)
	if len(r.Events) != 5 || r.Events[0].Type != EventEventsTruncated || r.Next != 7 {
		t.Errorf("after overflow: %+v", r)
	}
	// A cursor from a previous helper run (larger than seq) starts over.
	evs, next := bus.Since(100)
	if len(evs) != 4 || next != 7 {
		t.Errorf("stale cursor: %d events, next %d", len(evs), next)
	}
}

func TestLongPollEndsWhenClientLeaves(t *testing.T) {
	bus := NewBus(0)
	returned := make(chan struct{})
	sock, srv := startServer(t, AllowAll{}, func(s *Server) {
		Typed(s, MethodEventsWait, func(ctx context.Context, p EventsWaitParams) (EventsWaitResult, error) {
			bus.Wait(ctx, p.After, time.Minute)
			close(returned)
			return EventsWaitResult{}, nil
		})
	})
	c, err := Dial(context.Background(), nil, sock)
	if err != nil {
		t.Fatal(err)
	}
	go c.WaitEvents(context.Background(), 0, time.Minute)
	time.Sleep(50 * time.Millisecond)
	c.Close()
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("handler still waiting after client closed")
	}
	srv.Close()
}

func TestHandleRejectsUnlistedMethod(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic")
		}
	}()
	NewServer(NewUnixTransport(), AllowAll{}, nil).Handle("run.command", nil)
}

func TestListenStaleAndForeign(t *testing.T) {
	sock := shortSock(t)
	tr := NewUnixTransport()
	// Stale socket (listener gone without unlink).
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	l2, err := tr.Listen(sock)
	if err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	fi, _ := os.Stat(sock)
	if fi.Mode().Perm() != 0o660 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	// Live socket: refuse.
	if _, err := tr.Listen(sock); err == nil {
		t.Error("second listener on a live socket succeeded")
	}
	l2.Close()
	// A regular file is never removed.
	os.WriteFile(sock, []byte("keep"), 0o600)
	if _, err := tr.Listen(sock); err == nil {
		t.Error("listened over a regular file")
	}
	if b, _ := os.ReadFile(sock); string(b) != "keep" {
		t.Error("regular file was touched")
	}
}

func TestGroupAuthorizer(t *testing.T) {
	a := &GroupAuthorizer{GID: 80, SelfUID: 501, LookupGroups: func(uid int) ([]int, error) {
		if uid == 503 {
			return []int{20, 80}, nil
		}
		return []int{20}, nil
	}}
	cases := []struct {
		p  Peer
		ok bool
	}{
		{Peer{UID: 0}, true},
		{Peer{UID: 501}, true},
		{Peer{UID: 502, GIDs: []int{20, 80}}, true},
		{Peer{UID: 503, GIDs: []int{20}}, true}, // via lookup
		{Peer{UID: 504, GIDs: []int{20}}, false},
	}
	for _, c := range cases {
		if err := a.Authorize(c.p); (err == nil) != c.ok {
			t.Errorf("%+v: err = %v", c.p, err)
		}
	}
	none := &GroupAuthorizer{GID: -1, SelfUID: 501, LookupGroups: func(int) ([]int, error) { return []int{80}, nil }}
	if none.Authorize(Peer{UID: 502}) == nil {
		t.Error("group rule disabled but peer allowed")
	}
}
