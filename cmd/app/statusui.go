package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
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
	return ui.MenuButton(c.Key("recovery"), i18n.T.Recovery, func(m *ui.Menu) { a.recoveryItems(m, s) })
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
		if a.egressProbe == nil {
			return nil
		}
		return &fixAction{label: T.FixTestEgress, run: a.testEgress, off: a.egress.running}
	}
	return nil
}

// --- the egress test, on demand ---

// egressState is the last on-demand egress test (health.LeakEgress with
// the test turned on): two HTTPS requests to an IP-echo address, one
// direct and one through mihomo. It stands in for the check's 未开启 row.
type egressState struct {
	running bool
	result  *health.CheckReport
}

func (a *app) testEgress() {
	if a.egress.running || a.egressProbe == nil {
		return
	}
	a.egress.running = true
	probe := a.egressProbe
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cr := probe(ctx)
		mygo.RunOnMain(func() {
			a.egress.running = false
			a.egress.result = cr
			a.invalidate()
		})
	}()
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
	if has(status.ReasonMihomoDown) && can.Helper && a.hd.busy == "" {
		acts = append(acts, bannerAct{key: "start-mihomo", label: T.StartMihomo,
			run: func() { a.mihomoService(rpc.MethodServiceStart, T.StartMihomo) }})
	}
	if has(status.ReasonTSNeedsLogin) && can.Tailscale {
		acts = append(acts, bannerAct{key: "ts-login", label: T.TSLogIn, run: func() { a.tsLogin(s) }})
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
			pushFace(c, ui.MenuButton(c.Key(first.key), first.label, first.menu))
		} else if pushButton(c.Key(first.key), first.label).Clicked() {
			first.run()
		}
		if len(rest) == 0 {
			return
		}
		pushFace(c, ui.MenuButton(c.Key("banner-more"), i18n.T.SvcMore, func(m *ui.Menu) {
			for _, act := range rest {
				if act.menu != nil {
					m.Submenu(act.label, act.menu)
					continue
				}
				if m.Item(act.label).Chosen() {
					act.run()
				}
			}
		}))
	})
}

// tsLoginButton opens Tailscale's login page in the browser: the URL the
// node already has, else one it is asked for.
func (a *app) tsLoginButton(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	if pushButton(c.Key("ts-login"), T.TSLogIn).Disabled(!s.Can().Tailscale).Clicked() {
		a.tsLogin(s)
	}
}

func (a *app) tsLogin(s status.Snapshot) {
	if u := s.Tailscale.State.AuthURL; u != "" {
		_ = mygo.Shell.OpenExternal(u)
		return
	}
	ts, mon := a.ts, a.mon
	a.do(func(ctx context.Context) error {
		if err := ts.Login(ctx); err != nil {
			return err
		}
		// The node answers with its URL a moment later.
		for range 20 {
			st, err := ts.State(ctx)
			if err == nil && st.AuthURL != "" {
				u := st.AuthURL
				mygo.RunOnMain(func() { _ = mygo.Shell.OpenExternal(u) })
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
		mon.Refresh()
		return nil
	})
}

// gettingStarted is the 开始使用 strip while duet is not running the
// show yet: three steps, each done or not, each opening its page. It
// goes once all three are done.
func (a *app) gettingStarted(c *ui.Context, s status.Snapshot) {
	if h := s.Helper.State; h != status.HelperNotInstalled && (h != status.HelperConnected || s.Helper.Status == nil) {
		// Whether duet took over is known only from a connected helper
		// (or none installed): no strip that guesses.
		return
	}
	T := i18n.T
	installed := s.Helper.State != status.HelperNotInstalled
	taken := installed && s.TakenOver()
	// The step is done when nothing fails. Warnings stay on the health
	// page and badge but must not keep onboarding open forever: some
	// (network conditions, things the user chose) can't be cleared.
	_, fails := s.Health.Counts()
	clean := s.Health.Report != nil && fails == 0
	if installed && taken && clean {
		return
	}
	t := c.Theme()
	steps := []struct {
		label string
		done  bool
		page  string
	}{
		{T.StepHelper, installed, pageInstall},
		{T.StepTakeOver, taken, pageTakeover},
		{T.StepCheckCoexist, clean, pageHealth},
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		ui.Text(c, T.GetStarted).FontWeight(600).Margin(0, 4, 0, 0).SingleLine()
		for i, st := range steps {
			icon, col := iconSkip, t.TextMuted
			if st.done {
				icon, col = iconOK, t.Success
			}
			page := st.page
			b := pushFace(c.Key(fmt.Sprint("step", i)), ui.Button(c, "")).Label(st.label).Children(func() {
				ui.Icon(c, icon).FontSize(iconInline).TextColor(col)
				ui.Text(c, st.label).SingleLine()
			})
			if b.Clicked() {
				a.openPage(page)
			}
		}
	})
}

// overviewHelperCard is the overview's helper card, only while the helper
// is not connected (connected, the sidebar foot says so): its state and
// the one action that moves it on.
func (a *app) overviewHelperCard(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	h := s.Helper
	switch h.State {
	case status.HelperNotInstalled:
		card(c, T.Helper, func() {
			kvWith(c, T.State, func() {
				ui.Text(c, h.State.Text()).Grow(1).SingleLine()
				if pushButton(c.Key("helper-install"), T.FixInstallHelper).Disabled(!s.WritesAllowed).Clicked() {
					a.openPage(pageInstall)
				}
			})
		})
	case status.HelperNotRunning, status.HelperDenied:
		card(c, T.Helper, func() {
			kvWith(c, T.State, func() {
				ui.Text(c, h.State.Text()).Grow(1).SingleLine()
				if fileExists(a.helperLogFile()) && pushButton(c.Key("helper-log"), T.HelperViewLog).Clicked() {
					a.openHelperLog()
				}
				if busyButton(c.Key("helper-restart"), T.HelperRestart, false, a.hm.running == T.HelperRestart, !a.helperOpsEnabled(s)).Clicked() {
					a.restartHelper()
				}
			})
			if a.hm.errMsg != "" {
				errorNote(c, a.hm.errMsg)
			}
		})
	}
}
