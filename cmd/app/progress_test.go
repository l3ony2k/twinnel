package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// stageEvent is a compose.stage event.
func stageEvent(stage string) rpc.Event {
	b, _ := json.Marshal(rpc.ComposeStage{Stage: stage})
	return rpc.Event{Type: rpc.EventComposeStage, Data: b}
}

// TestTakeStages: stages set the apply's stage and never reach the log;
// the other events pass through in order.
func TestTakeStages(t *testing.T) {
	a := &app{onMain: func(func()) {}}
	evs := []rpc.Event{stageEvent(rpc.StageValidate), {Type: rpc.EventStateChanged}, stageEvent(rpc.StageReload)}
	rest := a.takeStages(evs)
	if len(rest) != 1 || rest[0].Type != rpc.EventStateChanged {
		t.Errorf("rest = %+v", rest)
	}
	if a.hd.applyStage != rpc.StageReload {
		t.Errorf("stage = %q, want the last one", a.hd.applyStage)
	}
	if evs[0].Type != rpc.EventComposeStage {
		t.Error("the caller's slice was changed")
	}
}

// TestComposeProgress: a running apply or preview says what it waits for,
// in both languages, once its spinner shows; nothing before that.
func TestComposeProgress(t *testing.T) {
	defer i18n.Set("zh")
	for _, lang := range []string{"zh", "en"} {
		i18n.Set(lang)
		T := i18n.T
		a := &app{}
		d := &a.hd
		d.busy, d.spanOp, d.span = T.Apply, T.Apply, busySpan{start: time.Now()}
		if got := a.composeProgress(); got != "" {
			t.Errorf("%s: %q before the spinner shows", lang, got)
		}
		d.span.start = time.Now().Add(-time.Second)
		for stage, want := range map[string]string{
			"":                T.StageValidate,
			rpc.StageValidate: T.StageValidate,
			rpc.StageWrite:    T.StageWrite,
			rpc.StageReload:   T.StageReload,
			rpc.StageVerify:   T.StageVerify,
		} {
			d.applyStage = stage
			if got := a.composeProgress(); got != want || got == "" {
				t.Errorf("%s: stage %q: %q, want %q", lang, stage, got, want)
			}
		}
		d.busy, d.spanOp = T.Preview, T.Preview
		if got := a.composeProgress(); got != T.StageValidate {
			t.Errorf("%s: preview: %q", lang, got)
		}
		a.hd.cfg.prevStage = stageDiff
		if got := a.composeProgress(); got != T.StageDiff {
			t.Errorf("%s: preview diff: %q", lang, got)
		}
	}
}

// TestComposeStatusFallback: compose.status where the helper has it;
// compose.preview's Changed from a helper that predates it.
func TestComposeStatusFallback(t *testing.T) {
	for _, newer := range []bool{true, false} {
		dir, err := os.MkdirTemp("", "twinnelstat") // short: unix socket paths are limited
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		sock := filepath.Join(dir, "h.sock")
		tr := rpc.NewUnixTransport()
		l, err := tr.Listen(sock)
		if err != nil {
			t.Fatal(err)
		}
		srv := rpc.NewServer(tr, allowPeers{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		previews := 0
		bus := rpc.NewBus(4)
		rpc.Typed(srv, rpc.MethodStatus, func(context.Context, rpc.Empty) (rpc.Status, error) { return rpc.Status{HelperVersion: "test"}, nil })
		rpc.Typed(srv, rpc.MethodEventsWait, func(ctx context.Context, p rpc.EventsWaitParams) (rpc.EventsWaitResult, error) {
			evs, next := bus.Wait(ctx, p.After, time.Duration(p.TimeoutMS)*time.Millisecond)
			return rpc.EventsWaitResult{Events: evs, Next: next}, nil
		})
		rpc.Typed(srv, rpc.MethodComposePreview, func(context.Context, rpc.Empty) (rpc.ComposeResult, error) {
			previews++
			return rpc.ComposeResult{Hash: "old", Changed: true}, nil
		})
		if newer {
			rpc.Typed(srv, rpc.MethodComposeStatus, func(context.Context, rpc.Empty) (rpc.ComposeStatus, error) {
				return rpc.ComposeStatus{Hash: "new", Changed: true}, nil
			})
		}
		go srv.Serve(l)
		ctx, cancel := context.WithCancel(context.Background())
		conn := &status.HelperConn{Socket: sock}
		go conn.Run(ctx)
		deadline := time.Now().Add(5 * time.Second)
		for !conn.State().Connected() {
			if time.Now().After(deadline) {
				t.Fatal("did not connect")
			}
			time.Sleep(10 * time.Millisecond)
		}
		r, err := composeStatus(ctx, conn)
		want := map[bool]string{true: "new", false: "old"}[newer]
		if err != nil || !r.Changed || r.Hash != want {
			t.Errorf("newer=%v: %+v %v", newer, r, err)
		}
		if newer && previews != 0 {
			t.Error("a helper with compose.status was asked for a preview")
		}
		cancel()
		l.Close()
		srv.Close()
	}
}
