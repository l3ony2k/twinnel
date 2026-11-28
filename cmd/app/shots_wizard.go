package main

import (
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/setup"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// wizardShots (TWINNEL_SHOTS_FLOW=wizard) captures every state of the
// first-run wizard on a fresh machine, once per system's wording
// (TWINNEL_SHOTS_OS: a comma-separated list of darwin, windows, linux;
// default all three; tsLoginPanel's own words follow TWINNEL_SHOTS_OS only
// when it names one system, so shoot one system per run for exact
// wording), named wiz-<os>-<state>.png. The states are set
// directly (setupView.shotSteps, wizardView.shot, a.shotSnap): no flow
// runs, nothing is installed, downloaded or written. The window's chrome
// is the host's; only the wizard's words change with the system.
func (a *app) wizardShots(save func(*mygo.Window, string)) {
	oses := []string{"darwin", "windows", "linux"}
	if v := os.Getenv("TWINNEL_SHOTS_OS"); v != "" {
		oses = strings.Split(v, ",")
	}
	for _, goos := range oses {
		a.wizardShotsFor(save, strings.TrimSpace(goos))
	}
	mygo.RunOnMain(func() {
		a.shotSnap, a.setupV.shotSteps, a.wiz.shot, a.wiz.goos = nil, nil, nil, ""
	})
}

// wizShot is one picture's state.
type wizShot struct {
	name   string
	step   int
	page   string // another page than the wizard (总览's 继续设置)
	helper bool   // the helper is connected
	steps  []setup.StepState
	facts  wizardShot
	health *health.Report
	// ts is Tailscale's state for tsLoginPanel: "none" (not installed),
	// "login" (installed, logged out), "running"; "" keeps the status
	// file's.
	ts    string
	setup func(a *app) // anything else (a URL typed, a file picked, …)
}

// wizShotList is every wizard state worth a picture, in goos's words
// (shared by the shot flow and the hermetic Windows renders,
// winshots_wizard_test.go).
func wizShotList(goos string) []wizShot {
	st := func(states map[setup.Step]setup.State) []setup.StepState {
		out := make([]setup.StepState, len(setup.Order))
		for i, id := range setup.Order {
			out[i] = setup.StepState{ID: id, State: setup.Pending}
			if s, ok := states[id]; ok {
				out[i].State = s
			}
		}
		return out
	}
	with := func(s []setup.StepState, id setup.Step, fn func(*setup.StepState)) []setup.StepState {
		for i := range s {
			if s[i].ID == id {
				fn(&s[i])
			}
		}
		return s
	}
	D, R, F, W, S := setup.Done, setup.Running, setup.Failed, setup.Waiting, setup.Skipped
	type M = map[setup.Step]setup.State
	helperOK := M{setup.StepHelper: D}
	mihomoOK := M{setup.StepHelper: D, setup.StepMihomo: D}
	proxyOK := M{setup.StepHelper: D, setup.StepMihomo: D, setup.StepSubscription: D, setup.StepConfig: D, setup.StepApply: D, setup.StepDNS: D}
	plus := func(m M, k setup.Step, v setup.State) M {
		out := M{}
		for kk, vv := range m {
			out[kk] = vv
		}
		out[k] = v
		return out
	}
	ver := func(s []setup.StepState) []setup.StepState {
		return with(with(s, setup.StepMihomo, func(s *setup.StepState) { s.Detail = "v1.19.34" }), setup.StepTailscale, func(s *setup.StepState) { s.Detail = "1.90.6" })
	}
	cfgPath := map[string]string{
		"darwin":  "~/.config/mihomo/config.yaml",
		"windows": `C:\Users\example\.config\mihomo\config.yaml`,
		"linux":   "~/.config/mihomo/config.yaml",
	}[goos]
	downloadErr := `Get "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest": dial tcp 192.0.2.10:443: i/o timeout`
	clean := &health.Report{Checks: []health.CheckReport{{ID: "a", Status: health.StatusOK}, {ID: "b", Status: health.StatusOK}}}
	issues := &health.Report{Checks: []health.CheckReport{{ID: "a", Status: health.StatusOK}, {ID: "b", Status: health.StatusFail}, {ID: "c", Status: health.StatusWarn}}}
	shots := []wizShot{
		{name: "welcome", step: wizWelcome, steps: st(nil)},
		{name: "helper", step: wizHelper, steps: st(nil)},
		{name: "helper-changes", step: wizHelper, steps: st(nil), setup: func(a *app) { a.wiz.planOpen = true; a.hd.onb.stepsOpen = true }},
		{name: "helper-running", step: wizHelper, steps: st(M{setup.StepHelper: R}), setup: func(a *app) { a.setupV.running = true }},
		{name: "helper-failed", step: wizHelper, steps: with(st(M{setup.StepHelper: F}), setup.StepHelper, func(s *setup.StepState) {
			s.Error = "twinnel: cancelled; nothing was changed"
		})},
		{name: "helper-done", step: wizHelper, helper: true, steps: st(helperOK)},
		{name: "mihomo", step: wizMihomo, helper: true, steps: st(helperOK)},
		{name: "mihomo-found", step: wizMihomo, helper: true, steps: st(helperOK), setup: func(a *app) {
			a.live = liveSetup{loaded: true, Config: cfgPath}
		}},
		{name: "mihomo-running", step: wizMihomo, helper: true, steps: st(plus(helperOK, setup.StepMihomo, R)), setup: func(a *app) { a.setupV.running = true }},
		{name: "mihomo-failed", step: wizMihomo, helper: true, steps: with(st(plus(helperOK, setup.StepMihomo, F)), setup.StepMihomo, func(s *setup.StepState) {
			s.Code, s.Error = setup.CodeDownload, downloadErr
		})},
		{name: "mihomo-failed-proxy", step: wizMihomo, helper: true, steps: with(st(plus(helperOK, setup.StepMihomo, F)), setup.StepMihomo, func(s *setup.StepState) {
			s.Code, s.Error = setup.CodeDownload, downloadErr
		}), setup: func(a *app) { a.setupV.proxyOpen, a.setupV.proxyAddr = true, "http://192.168.1.2:7890" }},
		{name: "mihomo-done", step: wizMihomo, helper: true, steps: ver(st(mihomoOK))},
		{name: "mihomo-needs-helper", step: wizMihomo, steps: st(nil)},
		{name: "sub", step: wizSub, helper: true, steps: ver(st(mihomoOK))},
		{name: "sub-url", step: wizSub, helper: true, steps: ver(st(mihomoOK)), setup: func(a *app) {
			a.setupV.url = "https://sub.example.com/api/v1/client/subscribe?token=synthetic"
		}},
		{name: "sub-imported", step: wizSub, helper: true, steps: ver(st(mihomoOK)), setup: func(a *app) {
			a.wiz.importName, a.wiz.importData = "twinnel-export.zip", []byte("synthetic")
		}},
		{name: "sub-running", step: wizSub, helper: true, steps: ver(st(plus(plus(mihomoOK, setup.StepSubscription, D), setup.StepConfig, R))), setup: func(a *app) {
			a.setupV.url, a.setupV.running = "https://sub.example.com/api/v1/client/subscribe?token=synthetic", true
		}},
		{name: "sub-starting", step: wizSub, helper: true, steps: ver(st(plus(plus(plus(mihomoOK, setup.StepSubscription, D), setup.StepConfig, D), setup.StepApply, R))), setup: func(a *app) {
			a.setupV.url, a.setupV.running, a.hd.applyStage = "https://sub.example.com/api/v1/client/subscribe?token=synthetic", true, rpc.StageReload
		}},
		{name: "sub-needs-url", step: wizSub, helper: true, steps: with(ver(st(plus(mihomoOK, setup.StepSubscription, W))), setup.StepSubscription, func(s *setup.StepState) {
			s.Code = setup.CodeNeedsSubscription
		}), setup: func(a *app) { a.wiz.importName, a.wiz.importData = "Merge.yaml", []byte("synthetic") }},
		{name: "sub-config-failed", step: wizSub, helper: true, steps: with(ver(st(plus(plus(mihomoOK, setup.StepSubscription, D), setup.StepConfig, F))), setup.StepConfig, func(s *setup.StepState) {
			s.Code, s.Error = setup.CodeInvalidConfig, "mihomo rejected the composed config: proxy group PROXY: proxy 'node-x' not found"
		}), setup: func(a *app) { a.setupV.url = "https://sub.example.com/api/v1/client/subscribe?token=synthetic" }},
		{name: "sub-done", step: wizSub, helper: true, steps: ver(st(proxyOK)), setup: func(a *app) {
			a.wiz.counted, a.wiz.nodes, a.wiz.rules = true, 128, 3412
		}},
		{name: "sub-download-later", step: wizMihomo, helper: true, steps: st(helperOK), setup: func(a *app) { a.wiz.viaSub = true }},
		{name: "ts", step: wizTS, helper: true, ts: "none", steps: ver(st(proxyOK))},
		{name: "ts-installing", step: wizTS, helper: true, ts: "none", steps: ver(st(plus(proxyOK, setup.StepTailscale, R))), setup: func(a *app) { a.setupV.running = true }},
		{name: "ts-failed", step: wizTS, helper: true, ts: "none", steps: with(ver(st(plus(proxyOK, setup.StepTailscale, F))), setup.StepTailscale, func(s *setup.StepState) {
			s.Code, s.Error = setup.CodeDownload, `Get "https://pkgs.tailscale.com/stable/": dial tcp 192.0.2.20:443: i/o timeout`
		})},
		{name: "ts-damaged", step: wizTS, helper: true, ts: "none", steps: with(ver(st(plus(proxyOK, setup.StepTailscale, F))), setup.StepTailscale, func(s *setup.StepState) {
			s.Code, s.Error = setup.CodeDamaged, `Tailscale is installed but its files are damaged: `+map[bool]string{true: `C:\Program Files\Tailscale\tailscale.exe`, false: `/usr/local/bin/tailscale`}[goos == "windows"]+` (zeroed); repair it to reinstall`
		})},
		{name: "ts-login", step: wizTS, helper: true, ts: "login", steps: ver(st(plus(proxyOK, setup.StepTailscale, D)))},
		{name: "ts-done", step: wizTS, helper: true, ts: "running", steps: ver(st(plus(plus(proxyOK, setup.StepTailscale, D), setup.StepLogin, D)))},
		{name: "done", step: wizDone, helper: true, steps: ver(st(plus(plus(proxyOK, setup.StepTailscale, D), setup.StepLogin, D))),
			ts: "running", facts: wizardShot{mihomoVer: "v1.19.34"}, health: clean,
			setup: func(a *app) { a.wiz.counted, a.wiz.nodes, a.wiz.rules = true, 128, 3412 }},
		{name: "done-issues", step: wizDone, helper: true, ts: "none", steps: ver(st(proxyOK)), facts: wizardShot{mihomoVer: "v1.19.34"}, health: issues,
			setup: func(a *app) { a.wiz.state.TSSkipped = true; a.wiz.counted, a.wiz.nodes = true, 128 }},
		{name: "done-checking", step: wizDone, helper: true, ts: "none", steps: ver(st(proxyOK)), facts: wizardShot{mihomoVer: "v1.19.34"},
			setup: func(a *app) { a.wiz.state.TSSkipped = true }},
		{name: "done-dns-manual", step: wizDone, helper: true, ts: "none", steps: with(ver(st(plus(proxyOK, setup.StepDNS, S))), setup.StepDNS, func(s *setup.StepState) { s.Code = setup.CodeUnsupported }),
			facts: wizardShot{mihomoVer: "v1.19.34"}, health: clean, setup: func(a *app) { a.wiz.state.TSSkipped = true; a.wiz.counted, a.wiz.nodes = true, 128 }},
		{name: "overview-resume", page: pageOverview, step: wizSub, helper: true, steps: ver(st(mihomoOK))},
		{name: "config-gate", page: pageConfig, step: wizHelper, steps: st(nil)},
	}
	return shots
}

func (a *app) wizardShotsFor(save func(*mygo.Window, string), goos string) {
	mygo.RunOnMain(func() { a.win.SetSize(a.winWidth(), 760) })
	for _, sh := range wizShotList(goos) {
		apply := func() {
			a.applyWizShot(sh, goos)
			a.win.Invalidate()
		}
		mygo.RunOnMain(func() {
			old := a.page
			a.page = pageWizard
			if sh.page != "" {
				a.page = sh.page
			}
			a.pageChanged(old)
			apply()
		})
		time.Sleep(700 * time.Millisecond)
		mygo.RunOnMain(apply)
		time.Sleep(300 * time.Millisecond)
		save(a.win, "wiz-"+goos+"-"+sh.name)
	}
}

// applyWizShot sets the app to sh's state. Main thread.
func (a *app) applyWizShot(sh wizShot, goos string) {
	a.wiz.goos = goos
	a.wiz.state = wizardState{Step: sh.step}
	facts := sh.facts
	a.wiz.shot = &facts
	a.wiz.planOpen, a.wiz.ownMih, a.wiz.viaSub = false, false, false
	a.wiz.counted, a.wiz.counting, a.wiz.nodes, a.wiz.rules = false, false, 0, 0
	a.wiz.importName, a.wiz.importData, a.wiz.subKernel = "", nil, false
	a.live = liveSetup{loaded: true} // nothing to take over
	a.setupV.shotSteps, a.setupV.url, a.setupV.running = sh.steps, "", false
	a.setupV.proxyOpen, a.setupV.proxyAddr = false, ""
	if sh.setup != nil {
		sh.setup(a)
	}
	connected, rep, tsState := sh.helper, sh.health, sh.ts
	a.shotSnap = func(s *status.Snapshot) {
		if connected {
			s.Helper = status.Helper{State: status.HelperConnected, Status: &rpc.Status{}}
		} else {
			s.Helper = status.Helper{State: status.HelperNotInstalled}
		}
		s.WritesAllowed = true
		s.Health = status.Health{Report: rep}
		switch tsState {
		case "none":
			s.Tailscale = status.Tailscale{Err: "tailscaled is not installed", Install: tailscale.Install{Kind: tailscale.InstallUnknown}}
		case "login":
			s.Tailscale.Reachable = true
			s.Tailscale.State.BackendState = "NeedsLogin"
			s.Tailscale.State.Version = "1.90.6"
		case "running":
			s.Tailscale.Reachable = true
			s.Tailscale.State.BackendState = "Running"
		}
	}
}
