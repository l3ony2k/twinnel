package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/setup"
	"github.com/l3ony2k/twinnel/core/status"
)

// setupShots (TWINNEL_SHOTS_FLOW=setup) captures 设置这台 Mac in each state
// and 开始使用 on a machine with nothing to take over. The steps are set
// directly (setupView.shotSteps): no flow runs, nothing is installed,
// downloaded or written, and the helper is shown as not installed.
func (a *app) setupShots(save func(*mygo.Window, string)) {
	st := func(states ...setup.State) []setup.StepState {
		out := make([]setup.StepState, len(setup.Order))
		for i, id := range setup.Order {
			out[i] = setup.StepState{ID: id, State: setup.Pending}
			if i < len(states) {
				out[i].State = states[i]
			}
		}
		return out
	}
	D, R, F, W, S := setup.Done, setup.Running, setup.Failed, setup.Waiting, setup.Skipped
	with := func(s []setup.StepState, id setup.Step, fn func(*setup.StepState)) []setup.StepState {
		for i := range s {
			if s[i].ID == id {
				fn(&s[i])
			}
		}
		return s
	}
	details := func(s []setup.StepState) []setup.StepState {
		for i := range s {
			if s[i].State != setup.Done {
				continue
			}
			switch s[i].ID {
			case setup.StepSubscription:
				s[i].Detail = "Subscription"
			case setup.StepMihomo, setup.StepApply:
				s[i].Detail = "v1.19.34"
			case setup.StepConfig:
				s[i].Detail = ""
			case setup.StepDNS:
				s[i].Detail = "127.0.0.1"
			case setup.StepTailscale:
				s[i].Detail = "1.104.7"
			case setup.StepLogin:
				s[i].Detail = "example.ts.net"
			case setup.StepHealth:
				s[i].Detail = "11/11"
			}
		}
		return s
	}
	type shot struct {
		name  string
		page  string
		url   string
		proxy bool
		steps []setup.StepState
	}
	shots := []shot{
		{"overview-fresh", pageOverview, "", false, st()},
		{"setup-start", pageSetup, "", false, st()},
		{"setup-url", pageSetup, "https://sub.example.com/api/v1/client/subscribe?token=synthetic", false, st(D)},
		{"setup-running", pageSetup, "", false, details(st(D, D, R))},
		{"setup-download-failed", pageSetup, "", false, with(details(st(D, D, F)), setup.StepMihomo, func(s *setup.StepState) {
			s.Code, s.Error = setup.CodeDownload, `Get "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest": dial tcp 192.0.2.10:443: i/o timeout`
		})},
		{"setup-download-proxy", pageSetup, "", true, with(details(st(D, D, F)), setup.StepMihomo, func(s *setup.StepState) {
			s.Code, s.Error = setup.CodeDownload, `Get "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest": dial tcp 192.0.2.10:443: i/o timeout`
		})},
		{"setup-login", pageSetup, "", false, with(details(st(D, D, D, D, D, D, D, W)), setup.StepLogin, func(s *setup.StepState) {
			s.URL = "https://login.tailscale.com/a/synthetic"
		})},
		{"setup-health-failed", pageSetup, "", false, with(details(st(D, D, D, D, D, D, D, D, F)), setup.StepHealth, func(s *setup.StepState) {
			s.Code, s.Error = setup.CodeHealthFail, "System DNS"
		})},
		{"setup-no-tailscale", pageSetup, "", false, with(details(st(D, D, D, D, D, S, S, S, D)), setup.StepMihomo, func(s *setup.StepState) {
			s.Warnings = []setup.Warning{{Code: setup.WarnGeo}}
		})},
		{"setup-done", pageSetup, "", false, details(st(D, D, D, D, D, D, D, D, D))},
	}
	mygo.RunOnMain(func() { a.win.SetSize(a.winWidth(), 900) })
	for _, sh := range shots {
		apply := func() {
			a.live = liveSetup{loaded: true} // nothing to take over
			a.setupV.shotSteps, a.setupV.url, a.setupV.proxyOpen = sh.steps, sh.url, sh.proxy
			if sh.proxy {
				a.setupV.proxyAddr = "http://192.168.1.2:7890"
			}
			a.shotSnap = func(s *status.Snapshot) {
				s.Helper = status.Helper{State: status.HelperNotInstalled}
				s.WritesAllowed = true
			}
			a.win.Invalidate()
		}
		mygo.RunOnMain(func() {
			old := a.page
			a.page = sh.page
			a.pageChanged(old)
			apply()
		})
		time.Sleep(900 * time.Millisecond)
		mygo.RunOnMain(apply)
		time.Sleep(300 * time.Millisecond)
		save(a.win, sh.name)
	}
	mygo.RunOnMain(func() { a.shotSnap = nil; a.setupV.shotSteps = nil })
}
