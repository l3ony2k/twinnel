package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/rpc/healthsrc"
	"github.com/l3ony2k/twinnel/core/status"
)

// Shared parts of the status surfaces (overview, flyout, sidebar foot,
// health page): the helper lock on controls that need it, the accepted
// exit node, mihomo's service actions, 应急恢复, and the overview's
// banner actions and 开始使用 strip.

// needsHelper: changes are allowed, but the helper that makes them is not
// connected. A control that needs it shows the lock (helperLock), not a
// sentence. In read-only development runs nothing shows a lock.
func needsHelper(s status.Snapshot) bool {
	return s.WritesAllowed && !s.Helper.Connected()
}

// helperLock draws a small lock after a control that needs the helper
// while it is not connected: the tooltip says so, a click opens where it
// gets installed or started. It reports whether it drew.
func (a *app) helperLock(c *ui.Context, s status.Snapshot) bool {
	if !needsHelper(s) {
		return false
	}
	t := c.Theme()
	T := i18n.T
	b := ui.Row(c.Key("helper-lock")).AlignItems(ui.Center).Justify(ui.Center).Size(20, 20).Radius(plat.controlRadius).
		Shrink(0).Tooltip(T.NeedsHelper).Label(T.NeedsHelper).Role(ui.RoleButton).Focusable()
	b.Children(func() {
		ui.Icon(c, iconLock).FontSize(iconSmall).TextColor(t.TextMuted)
	})
	if b.Hovered() {
		b.Background(t.Text.Alpha(0.08))
	}
	if b.Clicked() {
		a.openHelperSetup(s)
	}
	return true
}

// openHelperSetup opens where the helper gets installed (not installed)
// or managed (installed but down: 设置 → 后台助手).
func (a *app) openHelperSetup(s status.Snapshot) {
	if need, known := needsSetup(s); known && need {
		a.openWizard()
		return
	}
	if s.Helper.State == status.HelperNotInstalled {
		a.openPage(pageInstall)
		return
	}
	a.openPage(pageSettings)
}

// --- the accepted exit node ---

// acceptExitNode records that the user chose to use the exit node name
// while mihomo's TUN is on. Main thread.
func (a *app) acceptExitNode(name string) {
	a.setExitNodeAccepted(name)
}

func (a *app) setExitNodeAccepted(name string) {
	if a.cfg.ExitNodeAccepted == name {
		return
	}
	cfg := a.cfg
	cfg.ExitNodeAccepted = name
	if a.cfgPath != "" && !a.shotMode {
		if err := status.SaveConfig(a.cfgPath, cfg); err != nil {
			a.actionErr = i18n.T.SaveFailed + err.Error()
		}
	}
	a.cfg = cfg
	if a.mon != nil {
		mon := a.mon
		go mon.SetExitNodeAccepted(name)
	}
}

// syncExitNodeAcceptance clears the acceptance once the exit node it was
// for is no longer in use (changed or turned off). Only a running,
// reachable Tailscale counts: a blip must not forget the choice.
func (a *app) syncExitNodeAcceptance(s status.Snapshot) {
	acc := a.cfg.ExitNodeAccepted
	ts := s.Tailscale
	if acc == "" || !ts.Reachable || !ts.State.Running() {
		return
	}
	if ts.State.ExitNode == nil || ts.State.ExitNode.Name != acc {
		a.setExitNodeAccepted("")
	}
}

// turnOffExitNode stops using an exit node.
func (a *app) turnOffExitNode() {
	a.optimistic(pendExit, i18n.T.ExitNodeNone, i18n.T.ExitNodeFailed, func(ctx context.Context) error { return a.ts.SetExitNode(ctx, "") })
}

// exitNodeAcceptedRow is the overview's neutral row for an accepted exit
// node: what is true, and the way back.
func (a *app) exitNodeAcceptedRow(c *ui.Context, s status.Snapshot) {
	name, ok := s.ExitNodeAccepted()
	if !ok {
		return
	}
	t := c.Theme()
	T := i18n.T
	groupBox(c, t).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconSkip).FontSize(iconNav).TextColor(t.TextMuted).Shrink(0)
			ui.Text(c, fmt.Sprintf(T.ExitNodeAcceptedRow, name)).Shrink(1).Wrap().LineHeight(lineText)
			infoTipAbout(c, T.ExitNode, T.TipExitAccepted, nil)
			ui.Spacer(c)
			if pushButton(c.Key("exit-off"), T.ExitNodeTurnOff).Disabled(!s.Can().Tailscale).Clicked() {
				a.turnOffExitNode()
			}
		})
		a.controlError(c, pendExit)
	})
}

// --- mihomo's service, through the helper ---

// mihomoService starts or restarts mihomo's service (the adopted one
// included: the helper knows it).
func (a *app) mihomoService(method, label string) {
	a.helperOp(label, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, method, rpc.ServiceParams{Name: "mihomo"}, nil)
	}, func(err error) {
		// Shown where it was asked for: the flyout and the overview
		// (actionNotes), not only on the page that was open.
		a.actionErr = ""
		if err != nil {
			a.actionErr = helperError(err)
		}
		a.mon.Refresh()
	})
}

// mihomoServiceItems are the 操作 menu's items while mihomo is down.
func (a *app) mihomoServiceItems(m *ui.Menu, s status.Snapshot) {
	T := i18n.T
	off := !s.Can().Helper || a.hd.busy != ""
	if m.Item(T.StartMihomo).Disabled(off).Chosen() {
		a.mihomoService(rpc.MethodServiceStart, T.StartMihomo)
	}
	if m.Item(T.RestartMihomo).Disabled(off).Chosen() {
		a.mihomoService(rpc.MethodServiceRestart, T.RestartMihomo)
	}
}

// --- 应急恢复 ---

// recoveryMenu is 应急恢复: what still works when things are broken.
// Restarting mihomo goes through the helper; the exit node through
// Tailscale's LocalAPI; the connectivity test reads only.
func (a *app) recoveryMenu(c *ui.Context, s status.Snapshot) ui.Element {
	return pushMenu(c.Key("recovery"), i18n.T.Recovery, func(m *ui.Menu) { a.recoveryItems(m, s) })
}

// recoveryItems are 应急恢复's items (the overview's button, the flyout's
// row).
func (a *app) recoveryItems(m *ui.Menu, s status.Snapshot) {
	T := i18n.T
	if m.Item(T.RestartMihomo).Disabled(!s.Can().Helper || a.hd.busy != "").Chosen() {
		a.mihomoService(rpc.MethodServiceRestart, T.RestartMihomo)
	}
	hasExit := s.Tailscale.Reachable && s.Tailscale.State.ExitNode != nil
	if m.Item(T.ExitNodeTurnOff).Disabled(!s.Can().Tailscale || !hasExit).Chosen() {
		a.turnOffExitNode()
	}
	m.Separator()
	if m.Item(T.RecoveryConnectivity).Chosen() {
		a.connectivityTest(s)
	}
}

// connectivityTest reruns the health checks and tests the main group's
// nodes (reads only), then shows the health page.
func (a *app) connectivityTest(s status.Snapshot) {
	a.rerunHealth()
	if p := s.Mihomo.Primary; p != nil && s.Mihomo.Reachable {
		a.testGroup(p.Name)
	}
	a.openPage(pageHealth)
}

// --- fixes ---

// regenCoexist re-reads the tailnet in the helper, which regenerates the
// Tailscale coexistence override, then opens 订阅与覆写, where the change
// is previewed before it is applied.
func (a *app) regenCoexist() {
	T := i18n.T
	a.helperOp(T.FixRegenCoexist, time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodTailnetRefresh, rpc.Empty{}, nil)
	}, func(err error) {
		if err == nil {
			a.openPage(pageConfig)
		}
	})
}

// fixAction is the button a health check's fix code maps to in the
// current state, or nil when there is none: a sentence alone stays.
type fixAction struct {
	label string
	run   func()
	off   bool
}

func (a *app) fixAction(cr health.CheckReport, s status.Snapshot) *fixAction {
	T := i18n.T
	f := cr.Fix
	if f == nil {
		return nil
	}
	helperStep := func(label string, run func()) *fixAction {
		switch {
		case a.wizardPending(s) && !s.TakenOver(): // not set up yet: the wizard
			return &fixAction{label: T.WizResume, run: a.openWizard}
		case s.Helper.State == status.HelperNotInstalled:
			return &fixAction{label: T.FixInstallHelper, run: func() { a.openPage(pageInstall) }, off: !s.WritesAllowed}
		case !s.TakenOver():
			return &fixAction{label: T.FixTakeOver, run: func() { a.openPage(pageTakeover) }, off: !s.WritesAllowed}
		}
		return &fixAction{label: label, run: run, off: !s.Can().Helper || a.hd.busy != ""}
	}
	switch f.AutoFix {
	case health.FixCoexist:
		return helperStep(T.FixRegenCoexist, a.regenCoexist)
	case health.FixLeakGuard:
		return helperStep(T.FixOpenOverrides, func() { a.openPage(pageConfig) })
	case health.FixExitNodeOff:
		return &fixAction{label: T.ExitNodeTurnOff, run: a.turnOffExitNode, off: !s.Can().Tailscale}
	case health.FixEgressProbe:
		if !a.canTestEgress(s) {
			return nil
		}
		return &fixAction{label: T.FixTestEgress, run: a.testEgress, off: a.egress.running}
	}
	return nil
}

// --- the egress test, on demand ---

// egressState is the on-demand egress test (health.LeakEgress with the
// test turned on): two HTTPS requests to an IP-echo address, one direct
// and one through mihomo. With the helper connected it runs there
// (health.egress), which keeps the result in its reports; result is only
// the in-process run's, without a helper (or with one older than
// health.egress), standing in for the check's untested row.
type egressState struct {
	running bool
	result  *health.CheckReport
	err     string // the last run failed (raw, behind 复制详情): a failedRow under the row
	why     string // err in words (helperError)
}

// canTestEgress: the helper runs the test, or the app can.
func (a *app) canTestEgress(s status.Snapshot) bool {
	return s.Helper.Connected() && a.hc != nil && !a.shotMode || a.egressProbe != nil
}

// egressHelper is the helper connection the egress test may use: nil in
// shots (the fixture's probe, never the scratch helper's real test) and
// while it is not connected.
func (a *app) egressHelper() helperCaller {
	if a.hc == nil || a.shotMode || !a.hc.State().Connected() {
		return nil
	}
	return a.hc
}

// runEgress runs the egress test once: in the helper (health.egress) when
// hc is set, else in process with probe. local reports that the result is
// the in-process run's (the helper keeps its own in its reports). A helper
// that answers but has no egress check, or a probe that returns nothing,
// is an error: the button never just stops.
func runEgress(ctx context.Context, hc helperCaller, probe func(context.Context) *health.CheckReport) (cr *health.CheckReport, local bool, err error) {
	if hc != nil {
		var hr rpc.HealthReport
		err = hc.Call(ctx, rpc.MethodHealthEgress, rpc.HealthEgressParams{Lang: i18n.Tag(), IPs: true}, &hr)
		var re *rpc.Error
		if errors.As(err, &re) && re.Code == rpc.CodeInvalidParams {
			// A helper from before IPs: the test without the addresses.
			err = hc.Call(ctx, rpc.MethodHealthEgress, rpc.HealthEgressParams{Lang: i18n.Tag()}, &hr)
		}
		if errors.As(err, &re) && re.Code == rpc.CodeMethodNotFound && probe != nil {
			hc = nil // a helper older than health.egress: run it here
		} else if err != nil {
			return nil, false, err
		} else {
			rep, derr := healthsrc.Decode(hr)
			if derr != nil {
				return nil, false, derr
			}
			if rep != nil {
				for i := range rep.Checks {
					if rep.Checks[i].ID == (health.LeakEgress{}).ID() {
						return &rep.Checks[i], false, nil
					}
				}
			}
			return nil, false, errors.New("the helper's report has no egress check")
		}
	}
	if probe == nil {
		return nil, true, errors.New("the egress test is not available")
	}
	if cr = probe(ctx); cr == nil {
		return nil, true, errors.New("the egress test returned no result")
	}
	if cr.At.IsZero() {
		cr.At = time.Now()
	}
	return cr, true, nil
}

func (a *app) testEgress() {
	if a.egress.running {
		return
	}
	hc, probe := a.egressHelper(), a.egressProbe
	if hc == nil && probe == nil {
		return
	}
	a.egress.running, a.egress.err, a.egress.why = true, "", ""
	main := a.onMain
	if main == nil {
		main = mygo.RunOnMain
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
		defer cancel()
		cr, local, err := runEgress(ctx, hc, probe)
		main(func() {
			a.egress.running = false
			switch {
			case err != nil:
				a.egress.err, a.egress.why = err.Error(), helperError(err)
			case local:
				a.egress.result = cr
			default:
				a.egress.result = nil // the helper's report has it now
				if a.mon != nil {
					a.runHealth()
				}
			}
			a.invalidate()
		})
	}()
}

// setEgressPeriodic turns 定期检测 on or off in the helper's settings.
// Turning it on tests at once, so the row has a result to show.
func (a *app) setEgressPeriodic(on bool) {
	a.helperOp(i18n.T.EgressPeriodic, 10*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodSettingsUpdate, rpc.SettingsUpdate{HealthEgress: &on}, nil)
	}, func(err error) {
		if err != nil {
			a.toastErr = i18n.T.ErrorText(helperError(err))
			return
		}
		if on {
			a.testEgress()
		}
	})
}

// --- overview ---

// bannerAct is one thing the overview banner offers for its reasons.
type bannerAct struct {
	key, label string
	run        func()
	menu       func(m *ui.Menu) // a submenu (应急恢复) instead of run
}

// bannerActs are the banner's actions that can run now, most useful
// first: what fixes the worst reason leads. Actions that can't run are
// left out, not greyed (walkthrough 2, N8).
func (a *app) bannerActs(s status.Snapshot) []bannerAct {
	T := i18n.T
	has := func(code string) bool {
		for _, r := range s.Reasons {
			if r.Code == code {
				return true
			}
		}
		return false
	}
	can := s.Can()
	var acts []bannerAct
	if has(status.ReasonCtlForeign) {
		acts = append(acts, bannerAct{key: "foreign-fix", label: T.ForeignHowToFix, run: func() { a.openPage(pageHealth) }})
	}
	if has(status.ReasonMihomoDown) && can.Helper && a.hd.busy == "" {
		acts = append(acts, bannerAct{key: "start-mihomo", label: T.StartMihomo,
			run: func() { a.mihomoService(rpc.MethodServiceStart, T.StartMihomo) }})
	}
	if has(status.ReasonTSNeedsLogin) && can.Tailscale {
		switch tsPhaseOf(s) {
		case tsMachineAuth:
			acts = append(acts, bannerAct{key: "ts-admin", label: T.TSLAdminConsole, run: func() { _ = mygo.Shell.OpenExternal(tailscaleAdminURL) }})
		case tsKeyExpired:
			acts = append(acts, bannerAct{key: "ts-login", label: T.TSLRelogin, run: func() { a.tsLoginFromBanner(s) }})
		default:
			acts = append(acts, bannerAct{key: "ts-login", label: T.TSLLogIn, run: func() { a.tsLoginFromBanner(s) }})
		}
	}
	if has(status.ReasonHealth) {
		if !s.TakenOver() && s.WritesAllowed {
			acts = append(acts, bannerAct{key: "findings-takeover", label: T.FindingsTakeover, run: func() { a.openPage(pageTakeover) }})
		}
		acts = append(acts, bannerAct{key: "findings", label: T.FindingsView, run: func() { a.openPage(pageHealth) }})
	}
	if has(status.ReasonTSExitNode) && s.Tailscale.State.ExitNode != nil {
		name := s.Tailscale.State.ExitNode.Name
		acts = append(acts, bannerAct{key: "exit-accept", label: T.ExitNodeAccept, run: func() { a.acceptExitNode(name) }})
		if can.Tailscale {
			acts = append(acts, bannerAct{key: "exit-off", label: T.ExitNodeTurnOff, run: a.turnOffExitNode})
		}
	}
	if has(status.ReasonMihomoDown) {
		acts = append(acts, bannerAct{key: "recovery", label: T.Recovery, menu: func(m *ui.Menu) { a.recoveryItems(m, s) }})
	}
	return acts
}

// verdictActions are the overview banner's actions: the first as a
// button, the rest under 更多 (at most two controls, N8).
func (a *app) verdictActions(c *ui.Context, s status.Snapshot) {
	acts := a.bannerActs(s)
	if len(acts) == 0 {
		return
	}
	first, rest := acts[0], acts[1:]
	ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Margin(4, 0, 0, 0).Children(func() {
		if first.menu != nil {
			pushMenu(c.Key(first.key), first.label, first.menu)
		} else if pushButton(c.Key(first.key), first.label).Clicked() {
			first.run()
		}
		if len(rest) == 0 {
			return
		}
		pushMenu(c.Key("banner-more"), i18n.T.SvcMore, func(m *ui.Menu) {
			for _, act := range rest {
				if act.menu != nil {
					m.Submenu(act.label, act.menu)
					continue
				}
				if m.Item(act.label).Chosen() {
					act.run()
				}
			}
		})
	})
}

// tsLoginFromBanner starts the login and shows the Tailscale page,
// where the link and its QR code are.
func (a *app) tsLoginFromBanner(s status.Snapshot) {
	a.startTSLogin(s, false)
	a.openPage(pageTailscale)
}

// gettingStarted is 总览's way back into the first-run wizard while
// Twinnel is not set up (or the wizard was left before 完成): one quiet
// row, where the setup stands and 继续设置. It goes once set up.
func (a *app) gettingStarted(c *ui.Context, s status.Snapshot) {
	if !a.wizardPending(s) {
		return
	}
	t := c.Theme()
	T := i18n.T
	groupBox(c, t).Padding(10, groupPadX).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconTwinnel).FontSize(iconInline + 2).TextColor(t.Accent).Shrink(0)
			ui.Text(c, T.WizResumeHead).FontWeight(600).SingleLine().Shrink(1)
			num(small(c, i18n.F(T.WizProgress, a.wiz.state.Step+1, wizSteps))).SingleLine()
			ui.Spacer(c)
			if ui.PrimaryButton(c.Key("wiz-resume"), T.WizResume).Clicked() {
				a.openWizard()
			}
		})
	})
}
