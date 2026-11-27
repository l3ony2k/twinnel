package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

func TestTSPhaseOf(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(240*time.Hour)
	snap := func(fn func(ts *status.Tailscale)) status.Snapshot {
		s := status.Snapshot{Time: now}
		s.Tailscale.Reachable = true
		s.Tailscale.Install.Kind = tailscale.InstallTailscaled
		fn(&s.Tailscale)
		return s
	}
	for _, tc := range []struct {
		name string
		fn   func(ts *status.Tailscale)
		want tsPhase
	}{
		{"not installed", func(ts *status.Tailscale) {
			ts.Reachable, ts.Install.Kind, ts.Err = false, tailscale.InstallUnknown, "no such file"
		}, tsNotInstalled},
		{"daemon down", func(ts *status.Tailscale) { ts.Reachable, ts.Err = false, "connection refused" }, tsDaemonDown},
		{"running", func(ts *status.Tailscale) { ts.State.BackendState = "Running" }, tsRunning},
		{"stopped", func(ts *status.Tailscale) { ts.State.BackendState = "Stopped" }, tsStopped},
		{"starting", func(ts *status.Tailscale) { ts.State.BackendState = "Starting" }, tsStarting},
		{"no state", func(ts *status.Tailscale) { ts.State.BackendState = "NoState" }, tsStarting},
		{"needs login", func(ts *status.Tailscale) { ts.State.BackendState = "NeedsLogin" }, tsNeedsLogin},
		{"logged out", func(ts *status.Tailscale) {
			ts.State.BackendState, ts.State.Prefs.LoggedOut = "NeedsLogin", true
		}, tsLoggedOut},
		{"expired key", func(ts *status.Tailscale) {
			ts.State.BackendState, ts.State.KeyExpiry = "NeedsLogin", &past
		}, tsKeyExpired},
		{"expired self", func(ts *status.Tailscale) {
			ts.State.BackendState, ts.State.Self.Expired = "NeedsLogin", true
		}, tsKeyExpired},
		{"key still valid", func(ts *status.Tailscale) {
			ts.State.BackendState, ts.State.KeyExpiry = "NeedsLogin", &future
		}, tsNeedsLogin},
		{"machine auth", func(ts *status.Tailscale) { ts.State.BackendState = "NeedsMachineAuth" }, tsMachineAuth},
	} {
		if got := tsPhaseOf(snap(tc.fn)); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestOSText(t *testing.T) {
	defer func(o string) { tsOS = o }(tsOS)
	for os, want := range map[string]string{"darwin": "m", "windows": "w", "linux": "l"} {
		tsOS = os
		if got := osText("m", "w", "l"); got != want {
			t.Errorf("%s: %s", os, got)
		}
	}
	tsOS = "windows"
	if tsDownloadURL() != "https://tailscale.com/download/windows" {
		t.Error(tsDownloadURL())
	}
}

// fakeLogin is tailscaled behind the login flow: a login URL after
// start, then Running after polls more state reads.
type fakeLogin struct {
	mu      sync.Mutex
	backend string
	url     string
	starts  int
	reauths int
	polls   int
	runAt   int // the read on which the browser login completes; 0 never
	err     error
}

func (f *fakeLogin) start(_ context.Context, reauth bool) (rpc.TailscaleLoginResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return rpc.TailscaleLoginResult{}, f.err
	}
	f.starts++
	if reauth {
		f.reauths++
	}
	f.url = "https://login.tailscale.com/a/synthetic"
	return rpc.TailscaleLoginResult{BackendState: f.backend, AuthURL: f.url, Started: true}, nil
}

func (f *fakeLogin) state(context.Context) (rpc.TailscaleLoginResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
	if f.runAt > 0 && f.polls >= f.runAt {
		f.backend, f.url = "Running", ""
	}
	return rpc.TailscaleLoginResult{BackendState: f.backend, AuthURL: f.url, Tailnet: "example.ts.net"}, nil
}

// loginApp is an app whose main thread is the test's: RunOnMain calls
// queue on main, which the test drains.
func loginApp(be tsLoginBackend) (*app, chan func(), *[]string) {
	main := make(chan func(), 64)
	var opened []string
	a := &app{tsLoginBE: be, onMain: func(fn func()) { main <- fn }}
	a.openURL = func(u string) { opened = append(opened, u) }
	return a, main, &opened
}

func drainUntil(t *testing.T, main chan func(), done func() bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for !done() {
		select {
		case fn := <-main:
			fn()
		case <-deadline:
			t.Fatal("timed out")
		}
	}
}

func TestTSLoginFlowOpensTheLinkAndFinishes(t *testing.T) {
	i18n.Set("en")
	defer i18n.Set("zh")
	f := &fakeLogin{backend: "NeedsLogin", runAt: 2}
	a, main, opened := loginApp(f)
	s := status.Snapshot{WritesAllowed: true}
	a.startTSLogin(s, false)
	if a.tsl.state != flowStarting || !a.tsPanelShown(s) {
		t.Fatalf("state %v", a.tsl.state)
	}
	drainUntil(t, main, func() bool { return a.tsl.state == flowWaiting })
	if a.tsl.url == "" || len(*opened) != 1 {
		t.Fatalf("waiting: url %q opened %v", a.tsl.url, *opened)
	}
	if bm := a.tsl.qrBitmap(a.tsl.url); bm == nil {
		t.Fatal("no QR code")
	}
	drainUntil(t, main, func() bool { return a.tsl.state == flowIdle })
	if a.toast != "Logged in to Tailscale · example.ts.net" || len(*opened) != 1 {
		t.Fatalf("done: toast %q opened %v", a.toast, *opened)
	}
}

func TestTSLoginFlowCancel(t *testing.T) {
	f := &fakeLogin{backend: "NeedsLogin"}
	a, main, _ := loginApp(f)
	a.startTSLogin(status.Snapshot{WritesAllowed: true}, false)
	drainUntil(t, main, func() bool { return a.tsl.state == flowWaiting })
	a.cancelTSLogin()
	if a.tsl.state != flowIdle || a.tsl.url != "" {
		t.Fatalf("cancel: %+v", a.tsl)
	}
	// Late answers of the cancelled flow change nothing.
	time.Sleep(1200 * time.Millisecond)
	for len(main) > 0 {
		(<-main)()
	}
	if a.tsl.state != flowIdle {
		t.Fatalf("after cancel: %v", a.tsl.state)
	}
}

func TestTSLoginFlowFailureIsWorded(t *testing.T) {
	i18n.Set("en")
	defer i18n.Set("zh")
	f := &fakeLogin{backend: "NeedsLogin", err: rpc.Errorf(rpc.CodeFailed, "tailscaled: dial unix /var/run/tailscaled.socket: connect: no such file or directory")}
	a, main, _ := loginApp(f)
	a.startTSLogin(status.Snapshot{WritesAllowed: true}, false)
	drainUntil(t, main, func() bool { return a.tsl.state == flowFailed })
	if a.tsl.errMsg == "" || a.tsl.errMsg == a.tsl.raw || a.tsl.raw == "" {
		t.Fatalf("failure: %q / %q", a.tsl.errMsg, a.tsl.raw)
	}
}

func TestTSLoginDone(t *testing.T) {
	r := func(b, u string) rpc.TailscaleLoginResult {
		return rpc.TailscaleLoginResult{BackendState: b, AuthURL: u}
	}
	for _, tc := range []struct {
		r              rpc.TailscaleLoginResult
		reauth, sawURL bool
		want           bool
	}{
		{r("Running", ""), false, false, true},
		{r("Running", ""), true, false, false}, // reauth: not before the link was used
		{r("Running", ""), true, true, true},
		{r("Running", "https://x"), true, true, false},
		{r("NeedsLogin", "https://x"), false, true, false},
		{r("NeedsMachineAuth", ""), false, true, true},
		{r("Starting", ""), false, false, true},
	} {
		if got := tsLoginDone(tc.r, tc.reauth, tc.sawURL); got != tc.want {
			t.Errorf("%+v reauth=%v saw=%v: %v", tc.r, tc.reauth, tc.sawURL, got)
		}
	}
}
