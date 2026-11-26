package main

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/engine"
	"github.com/l3ony2k/duet/core/engine/enginetest"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/store"
)

// TestHelperEndToEnd starts the real daemon wiring (run) as the current
// user on a temp state dir, with a fake controller and a fake mihomo, and
// drives it over the unix socket.
func TestHelperEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket + shell fake")
	}
	dir, err := os.MkdirTemp("", "duethelper") // short: sun_path limit
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	stateDir := filepath.Join(dir, "state")
	sock := filepath.Join(dir, "h.sock")

	ctrl := enginetest.NewController("dev-secret")
	srv := httptest.NewServer(ctrl)
	defer srv.Close()
	bin, _ := enginetest.WriteFakeMihomo(dir)
	confDir := filepath.Join(dir, "conf")

	st, _ := store.Open(stateDir)
	st.UpdateSettings(func(s *store.Settings) error {
		s.Mihomo.Binary = bin
		s.Mihomo.ConfigDir = confDir
		s.Mihomo.ConfigFile = filepath.Join(confDir, "config.yaml")
		s.Mihomo.Controller = srv.Listener.Addr().String()
		s.Tailscale.Coexist = true
		return nil
	})
	(&store.FileSecrets{Path: st.Path("secrets.json")}).Set(store.SecretController, "dev-secret")

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	ready := make(chan *engine.Engine, 1)
	go func() {
		runErr <- run(ctx, &options{
			stateDir: stateDir, socket: sock, foreground: false, group: "admin",
			tailnetStatus: "../../core/engine/testdata/tailscale-status.json",
			tailnetIface:  "none", noHealth: true,
		}, func(e *engine.Engine) {
			e.VerifyTimeout = time.Second
			ready <- e
		})
	}()
	select {
	case <-ready:
	case err := <-runErr:
		t.Fatalf("helper exited: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("helper did not start")
	}

	c, err := rpc.Dial(context.Background(), nil, sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	call := func(method string, params, result any) {
		t.Helper()
		if err := c.Call(context.Background(), method, params, result); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}

	var status rpc.Status
	call(rpc.MethodStatus, nil, &status)
	if !status.Mihomo.Reachable || status.Mihomo.Version != "v1.19.34" || status.UID != os.Getuid() {
		t.Errorf("status: %+v", status)
	}

	sub, _ := os.ReadFile("../../core/engine/testdata/subscription.yaml")
	call(rpc.MethodSubPut, rpc.SubPutParams{ID: "main", Content: string(sub)}, nil)
	call(rpc.MethodLayerPut, rpc.LayerPutParams{ID: "base", Stage: "base", Content: "mixed-port: 7890\nexternal-controller: " + srv.Listener.Addr().String() + "\n"}, nil)
	call(rpc.MethodLayerPut, rpc.LayerPutParams{ID: "direct", Content: "+rules:\n  - DOMAIN-SUFFIX,example.org,DIRECT\n"}, nil)

	var after uint64
	{
		var r rpc.EventsWaitResult
		call(rpc.MethodEventsWait, rpc.EventsWaitParams{After: 0, TimeoutMS: 1}, &r)
		after = r.Next
	}

	var ar rpc.ApplyResult
	call(rpc.MethodComposeApply, rpc.ApplyParams{Reason: "e2e"}, &ar)
	if !ar.Written || !ar.Reloaded || ar.HistoryID == "" {
		t.Fatalf("apply: %+v", ar)
	}
	disk, _ := os.ReadFile(filepath.Join(confDir, "config.yaml"))
	if !bytes.Contains(disk, []byte("DOMAIN,nas,DIRECT")) || !bytes.Contains(disk, []byte("secret: dev-secret")) {
		t.Error("composed config incomplete")
	}

	var evs rpc.EventsWaitResult
	call(rpc.MethodEventsWait, rpc.EventsWaitParams{After: after, TimeoutMS: 2000}, &evs)
	if len(evs.Events) == 0 || evs.Events[len(evs.Events)-1].Type != rpc.EventComposeApplied {
		t.Errorf("events: %+v", evs)
	}

	var hist []rpc.HistoryEntry
	call(rpc.MethodHistoryList, nil, &hist)
	if len(hist) != 1 || hist[0].ID != ar.HistoryID {
		t.Errorf("history: %+v", hist)
	}

	// Allowlist and validation over the wire.
	var re *rpc.Error
	if err := c.Call(context.Background(), "system.exec", map[string]string{"cmd": "id"}, nil); !errors.As(err, &re) || re.Code != rpc.CodeMethodNotFound {
		t.Errorf("unknown method: %v", err)
	}
	if err := c.Call(context.Background(), rpc.MethodLayerGet, map[string]string{"id": "../../etc/passwd"}, nil); err == nil {
		t.Error("traversal id accepted")
	}
	if err := c.Call(context.Background(), rpc.MethodSettingsUpdate, map[string]any{"config_file": "/etc/hosts"}, nil); !errors.As(err, &re) || re.Code != rpc.CodeInvalidParams {
		t.Errorf("path setting over RPC: %v", err)
	}

	// The socket is group-accessible, not world-accessible.
	fi, _ := os.Stat(sock)
	if fi.Mode().Perm() != 0o660 {
		t.Errorf("socket mode %v", fi.Mode().Perm())
	}
	logb, _ := os.ReadFile(filepath.Join(stateDir, "logs", "helper.log"))
	if !strings.Contains(string(logb), "helper started") || strings.Contains(string(logb), "dev-secret") {
		t.Errorf("log: %s", logb)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("run: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("helper did not stop")
	}
	if _, err := os.Stat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Error("socket not removed on shutdown")
	}
}

func TestParseFlags(t *testing.T) {
	o, err := parseFlags([]string{"--state-dir", "/tmp/x", "--socket", "/tmp/x/duet.sock", "--foreground"})
	if err != nil || o.stateDir != "/tmp/x" || o.socket != "/tmp/x/duet.sock" || !o.foreground || o.group != "admin" {
		t.Errorf("%+v %v", o, err)
	}
	if _, err := parseFlags([]string{"extra"}); err == nil {
		t.Error("extra args accepted")
	}
}
