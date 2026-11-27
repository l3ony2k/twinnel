package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// tsLoginShots (TWINNEL_SHOTS_FLOW=tslogin) captures every state of the
// Tailscale panel (tslogin.go) on the Tailscale page, the overview and
// the flyout, in the macOS, Windows and Linux wording (tsOS). Each state
// is the fake snapshot with its Tailscale fields overridden in memory
// (shotSnap) and, for the login under way, a synthetic flow: nothing
// talks to tailscaled, nothing opens a browser (openExternal).
func (a *app) tsLoginShots(save func(*mygo.Window, string)) {
	connected := status.Helper{State: status.HelperConnected, Status: &rpc.Status{HelperVersion: "0.1.0"}}
	const url = "https://login.tailscale.com/a/1f2e3d4c5b6a7"
	node := func(backend string) func(s *status.Snapshot) {
		return func(s *status.Snapshot) {
			s.Tailscale.State.BackendState = backend
			s.Tailscale.State.ExitNode = nil
		}
	}
	type state struct {
		name string
		snap func(s *status.Snapshot)
		flow tsLoginFlow
		os   bool // the wording differs by system: shoot all three
	}
	states := []state{
		{name: "not-installed", os: true, snap: func(s *status.Snapshot) {
			s.Tailscale = status.Tailscale{Err: "dial unix /var/run/tailscaled.socket: connect: no such file or directory",
				Install: tailscale.Install{Kind: tailscale.InstallUnknown}}
		}},
		{name: "daemon-down", os: true, snap: func(s *status.Snapshot) {
			s.Tailscale = status.Tailscale{Err: "dial unix /var/run/tailscaled.socket: connect: connection refused",
				Install: tailscale.Install{Kind: tailscale.InstallTailscaled}}
		}},
		{name: "daemon-down-macapp", snap: func(s *status.Snapshot) {
			s.Tailscale = status.Tailscale{Err: "connection refused",
				Install: tailscale.Install{Kind: tailscale.InstallMacStandalone}}
		}},
		{name: "starting", snap: node("Starting")},
		{name: "stopped", snap: node("Stopped")},
		{name: "needs-login", snap: node("NeedsLogin")},
		{name: "logged-out", snap: func(s *status.Snapshot) {
			node("NeedsLogin")(s)
			s.Tailscale.State.Prefs.LoggedOut = true
		}},
		{name: "key-expired", snap: func(s *status.Snapshot) {
			node("NeedsLogin")(s)
			past := s.Time.Add(-26 * time.Hour)
			s.Tailscale.State.KeyExpiry = &past
		}},
		{name: "machine-auth", snap: node("NeedsMachineAuth")},
		{name: "login-fetching", snap: node("NeedsLogin"), flow: tsLoginFlow{state: flowStarting}},
		{name: "login-waiting", snap: node("NeedsLogin"), flow: tsLoginFlow{state: flowWaiting, url: url}},
		{name: "login-timeout", snap: node("NeedsLogin"), flow: tsLoginFlow{state: flowTimedOut}},
		{name: "login-failed", snap: node("NeedsLogin"), flow: tsLoginFlow{state: flowFailed,
			errMsg: "", raw: "tailscaled: Access denied: profiles access denied"}},
		{name: "running-key-expiring", snap: func(s *status.Snapshot) {
			soon := s.Time.Add(3 * 24 * time.Hour)
			s.Tailscale.State.KeyExpiry = &soon
		}},
	}
	set := func(st state) {
		mygo.RunOnMain(func() {
			a.shotSnap = func(s *status.Snapshot) {
				s.Helper = connected
				s.WritesAllowed = true // the actions as a user sees them; nothing runs
				s.AcceptedExitNode = "exit-sg"
				st.snap(s)
				s.Loaded = true
				status.Evaluate(s)
			}
			f := st.flow
			if f.state == flowFailed && f.errMsg == "" {
				f.errMsg = helperError(rpc.Errorf(rpc.CodeFailed, "%s", f.raw))
			}
			f.gen = a.tsl.gen + 1
			a.tsl = f
			a.invalidate()
		})
	}
	page := func(p string) {
		mygo.RunOnMain(func() {
			old := a.page
			a.page = p
			a.pageChanged(old)
			a.win.Invalidate()
		})
		time.Sleep(700 * time.Millisecond)
	}
	panel := func(name string) {
		mygo.RunOnMain(func() {
			if a.panel != nil {
				a.panel.Invalidate()
				a.panel.CapturePage() // lay out and fit (fitShotPanel)
			}
		})
		time.Sleep(500 * time.Millisecond)
		var w *mygo.Window
		mygo.RunOnMain(func() { w = a.panel })
		if w != nil {
			save(w, name)
		}
	}
	mygo.RunOnMain(a.togglePanel)
	time.Sleep(1200 * time.Millisecond)
	for _, st := range states {
		oses := []string{"darwin"}
		if st.os {
			oses = []string{"darwin", "windows", "linux"}
		}
		for _, goos := range oses {
			mygo.RunOnMain(func() { tsOS = goos })
			set(st)
			suffix := ""
			if st.os {
				suffix = "-" + goos
			}
			page(pageTailscale)
			save(a.win, "ts-"+st.name+suffix+"-page")
			page(pageOverview)
			save(a.win, "ts-"+st.name+suffix+"-overview")
			panel("ts-" + st.name + suffix + "-flyout")
		}
	}
	mygo.RunOnMain(func() { a.tsl = tsLoginFlow{gen: a.tsl.gen + 1} })
}
