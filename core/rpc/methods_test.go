package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestEveryMethodHasAClass(t *testing.T) {
	if len(AllMethods) != len(classes) {
		t.Fatalf("%d methods, %d classes", len(AllMethods), len(classes))
	}
	for _, m := range AllMethods {
		switch ClassOf(m) {
		case ClassRead, ClassChange, ClassWait:
		default:
			t.Errorf("%s has no class", m)
		}
	}
	if ClassOf("nope.nope") != 0 {
		t.Error("an unknown method has a class")
	}
}

// What clients poll must be a read, so polling never floods helper.log.
func TestPolledMethodsAreReads(t *testing.T) {
	for _, m := range []string{MethodStatus, MethodServiceLog, MethodServiceStatus, MethodHealthLast, MethodHelperInfo, MethodDNSStatus, MethodSettingsGet, MethodLayerList, MethodSubList} {
		if ClassOf(m) != ClassRead {
			t.Errorf("%s: class %s, want read", m, ClassOf(m))
		}
	}
	if ClassOf(MethodEventsWait) != ClassWait {
		t.Error("events.wait is not a wait")
	}
	for _, m := range []string{MethodComposeApply, MethodControllerCredentials, MethodConfigExport, MethodAgentRecord} {
		if ClassOf(m) != ClassChange {
			t.Errorf("%s: class %s, want change", m, ClassOf(m))
		}
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// A read is logged at DEBUG, a change at INFO, the long poll never; the
// handler sees the peer in its context.
func TestCallLogLevelsAndPeer(t *testing.T) {
	var buf syncBuf
	tr, sock := testTransport(t)
	s := NewServer(tr, AllowAll{}, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	var ctxPeer Peer
	var hadPeer bool
	s.Handle(MethodStatus, func(ctx context.Context, _ Peer, _ json.RawMessage) (any, error) { return Status{}, nil })
	s.Handle(MethodLayerDelete, func(ctx context.Context, _ Peer, _ json.RawMessage) (any, error) {
		ctxPeer, hadPeer = PeerFrom(ctx)
		return Empty{}, nil
	})
	s.Handle(MethodEventsWait, func(ctx context.Context, _ Peer, _ json.RawMessage) (any, error) { return EventsWaitResult{}, nil })
	l, err := tr.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(l)
	t.Cleanup(func() { l.Close(); s.Close() })
	c := dial(t, sock)
	ctx := context.Background()
	for _, m := range []string{MethodStatus, MethodStatus, MethodEventsWait, MethodLayerDelete} {
		if err := c.Call(ctx, m, nil, nil); err != nil {
			t.Fatal(m, err)
		}
	}
	out := buf.String()
	if strings.Contains(out, "method="+MethodStatus) || strings.Contains(out, "method="+MethodEventsWait) {
		t.Errorf("a read or the long poll logged at INFO:\n%s", out)
	}
	if strings.Count(out, "method="+MethodLayerDelete) != 1 {
		t.Errorf("the change is not logged once:\n%s", out)
	}
	if strings.Contains(out, "uid=-1") {
		t.Errorf("uid=-1 in the log:\n%s", out)
	}
	if !hadPeer || ctxPeer.Caller() == "unknown" {
		t.Errorf("no peer in the handler's context: %v %+v", hadPeer, ctxPeer)
	}
	if !strings.Contains(out, "peer.pid=") {
		t.Errorf("the change's line does not name the caller:\n%s", out)
	}
}

func TestPeerCallerAndLogValue(t *testing.T) {
	for _, tc := range []struct {
		p          Peer
		caller     string
		has, hasnt []string
	}{
		{Peer{UID: 501, PID: 42, User: "alice"}, "alice (uid 501)", []string{"peer.uid=501", "peer.user=alice", "peer.pid=42"}, []string{"sid"}},
		{Peer{UID: 0, PID: 7}, "uid 0", []string{"peer.uid=0"}, []string{"user"}},
		{Peer{UID: -1, PID: 9, SID: "S-1-5-21-1-2-3-1001", User: `PC\alice`, Elevated: true}, `PC\alice (S-1-5-21-1-2-3-1001)`,
			[]string{"peer.sid=S-1-5-21-1-2-3-1001", `peer.user=PC\alice`, "peer.pid=9", "peer.elevated=true"}, []string{"uid"}},
		{Peer{UID: -1, PID: 9}, "pid 9", []string{"peer.pid=9"}, []string{"uid"}},
		{Peer{UID: -1}, "unknown", nil, []string{"uid"}},
	} {
		if got := tc.p.Caller(); got != tc.caller {
			t.Errorf("Caller(%+v) = %q, want %q", tc.p, got, tc.caller)
		}
		var b bytes.Buffer
		slog.New(slog.NewTextHandler(&b, nil)).Info("x", "peer", tc.p)
		for _, s := range tc.has {
			if !strings.Contains(b.String(), s) {
				t.Errorf("%+v: %q lacks %q", tc.p, b.String(), s)
			}
		}
		for _, s := range tc.hasnt {
			if strings.Contains(b.String(), "peer."+s) {
				t.Errorf("%+v: %q has %q", tc.p, b.String(), s)
			}
		}
	}
}

// The helper names itself the same way it names callers: never uid -1.
func TestSelf(t *testing.T) {
	s := Self()
	if s.PID == 0 || s.Caller() == "unknown" || strings.HasPrefix(s.Caller(), "pid ") {
		t.Fatalf("Self() = %+v, caller %q", s, s.Caller())
	}
	var b bytes.Buffer
	slog.New(slog.NewTextHandler(&b, nil)).Info("x", "as", s)
	if strings.Contains(b.String(), "uid=-1") {
		t.Fatal(b.String())
	}
}
