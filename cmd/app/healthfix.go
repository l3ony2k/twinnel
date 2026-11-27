package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/healthfix"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// Health fixes (health-fixes): every failing or warning check shows its
// reasons in plain words, each with its action: 修复 (a one-click fix,
// previewed in a sheet, applied through the helper or the app's own
// mihomo/Tailscale client, then checked again in place, with 撤销修复
// where it can be put back), 去调整 (the page and control where the user
// changes it) or 为什么无法自动修复 (why, and what to do outside). The
// fixes themselves are core/healthfix, shared with the CLI and MCP.

// fixState is the health page's fix state. Main thread.
type fixState struct {
	plan     *healthfix.Plan // the preview sheet's plan; open while set
	title    string          // its check's title
	loading  string          // check ID whose preview is being prepared
	running  bool
	err      string                     // the last run failed: in the sheet
	undo     map[string]*healthfix.Undo // check ID → its last fix's undo
	why      map[string]bool            // check|reason → 为什么无法自动修复 open
	spot     string                     // a guided fix's control, ringed for a moment
	spotTill time.Time
	// shotHelper stands in for the helper in shots (synthetic answers).
	shotHelper healthfix.Caller
}

// rowFix is one fix the row offers, without asking the helper.
type rowFix struct {
	reason string
	spec   health.FixSpec
	text   health.FixText
}

// rowFixes are cr's fixes, one per distinct fix, in its reasons' order.
func rowFixes(cr health.CheckReport, platform string) []rowFix {
	var out []rowFix
	seen := map[string]bool{}
	for _, r := range cr.Reasons {
		spec, ok := health.FixFor(r, platform)
		if !ok {
			continue
		}
		key := string(spec.Kind) + "|" + spec.Action + "|" + spec.Target
		if spec.Kind == health.FixManual {
			key += "|" + r
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		text, _ := health.FixTextFor(i18n.Tag(), platform, r)
		out = append(out, rowFix{reason: r, spec: spec, text: text})
	}
	return out
}

func reportPlatform(s status.Snapshot) string {
	if r := s.Health.Report; r != nil && r.Platform != "" {
		return r.Platform
	}
	return runtime.GOOS
}

// fixer is the Fixer for this run: the helper when connected (a fake in
// shots), the app's controller and Tailscale clients when it may write.
func (a *app) fixer(s status.Snapshot) *healthfix.Fixer {
	f := &healthfix.Fixer{Lang: i18n.Tag(), Platform: reportPlatform(s)}
	switch {
	case a.fx.shotHelper != nil:
		f.Helper = a.fx.shotHelper
	case a.hc != nil && !a.shotMode && a.hc.State().Connected():
		f.Helper = a.hc
	}
	if s.WritesAllowed && !a.shotMode {
		if a.mh != nil {
			f.Mihomo = healthfix.MihomoClient(a.mh)
		}
		if a.ts != nil {
			f.Tailscale = healthfix.TailscaleClient(a.ts)
		}
	}
	if f.Helper == nil {
		f.Rerun = func(context.Context) (*health.Report, error) { return nil, nil } // the monitor reruns in-process
	}
	return f
}

// needsHelperAction: the fix runs through the helper.
func needsHelperAction(action string) bool {
	switch action {
	case health.FixModeRule, health.FixExitNodeOff, health.FixRerun, health.FixEgressProbe:
		return false
	}
	return true
}

// configAction: the fix composes and applies the config (needs a
// takeover; shows the apply's stages while it runs).
func configAction(action string) bool {
	switch action {
	case health.FixCoexist, health.FixLeakGuard, health.FixControllerLoopback, health.FixController, health.FixTUNStack, health.FixControllerPort:
		return true
	}
	return false
}

// fixButton is a one-click fix's button in the current state: a step
// before it (set up, install the helper, take over) when that is missing.
func (a *app) fixButton(cr health.CheckReport, rf rowFix, s status.Snapshot) *fixAction {
	T := i18n.T
	act := rf.spec.Action
	switch act {
	case health.FixRerun:
		return &fixAction{label: T.FixRerun, run: a.rerunHealth, off: s.Health.Running}
	case health.FixEgressProbe:
		if !a.canTestEgress(s) {
			return nil
		}
		return &fixAction{label: T.FixDo, run: a.testEgress, off: a.egress.running}
	}
	if needsHelperAction(act) && a.fx.shotHelper == nil {
		switch {
		case a.wizardPending(s) && !s.TakenOver(): // not set up yet: the wizard (as fixAction)
			return &fixAction{label: T.WizResume, run: a.openWizard}
		case s.Helper.State == status.HelperNotInstalled:
			return &fixAction{label: T.FixInstallHelper, run: func() { a.openPage(pageInstall) }, off: !s.WritesAllowed}
		case configAction(act) && !s.TakenOver():
			return &fixAction{label: T.FixTakeOver, run: func() { a.openPage(pageTakeover) }, off: !s.WritesAllowed}
		}
	}
	off := a.fx.running || a.fx.loading != "" || a.hd.busy != ""
	if needsHelperAction(act) && a.fx.shotHelper == nil {
		off = off || !s.Can().Helper
	}
	if act == health.FixModeRule {
		off = off || !s.Can().Mihomo
	}
	if act == health.FixExitNodeOff {
		off = off || !s.Can().Tailscale
	}
	return &fixAction{label: T.FixDo, run: func() { a.previewFix(cr, rf.reason, s) }, off: off}
}

// previewFix prepares the plan (it may ask the helper) and opens the
// sheet on it.
func (a *app) previewFix(cr health.CheckReport, reason string, s status.Snapshot) {
	f := a.fixer(s)
	a.fx.loading, a.fx.err = cr.ID, ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		p, err := f.Plan(ctx, cr, reason)
		mygo.RunOnMain(func() {
			a.fx.loading = ""
			if err != nil {
				a.toastErr = fmt.Sprintf(i18n.T.FixFailed, cr.Title)
				a.invalidate()
				return
			}
			a.fx.plan, a.fx.title = &p, cr.Title
			a.invalidate()
		})
	}()
}

// runFix applies the sheet's plan, then shows the check's new result in
// place (the monitor's report) and keeps the undo on the row.
func (a *app) runFix(s status.Snapshot) {
	p := a.fx.plan
	if p == nil || a.fx.running {
		return
	}
	f := a.fixer(s)
	title := a.fx.title
	a.fx.running, a.fx.err = true, ""
	a.hd.applyStage = "" // the apply's stages (compose.stage) say how far it is
	a.beginOp(i18n.T.FixRunning)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		out, err := f.Apply(ctx, *p)
		mygo.RunOnMain(func() {
			a.fx.running = false
			a.endOp()
			T := i18n.T
			if err != nil {
				a.fx.err = helperError(err)
				a.invalidate()
				return
			}
			if a.fx.undo == nil {
				a.fx.undo = map[string]*healthfix.Undo{}
			}
			a.fx.undo[p.Check] = out.Undo
			a.fx.plan = nil
			if out.Check != nil && out.Check.Status != health.StatusOK && out.Check.Status != health.StatusSkip {
				a.toast = fmt.Sprintf(T.FixStill, title)
			} else {
				a.toast = fmt.Sprintf(T.FixDone, title)
			}
			a.afterFix(out.Check)
			a.invalidate()
		})
	}()
}

// afterFix shows the check's new result: the helper ran the checks, so
// the monitor reads its report; without the helper they run here.
func (a *app) afterFix(cr *health.CheckReport) {
	if a.fx.shotHelper != nil {
		return // shots show the result through shotSnap
	}
	if a.fixerHasHelper() {
		a.runHealth()
		return
	}
	a.rerunHealth()
}

func (a *app) fixerHasHelper() bool {
	return a.hc != nil && !a.shotMode && a.hc.State().Connected()
}

// undoFix puts a check's last fix back.
func (a *app) undoFix(id string, s status.Snapshot) {
	u := a.fx.undo[id]
	if u == nil || a.fx.running {
		return
	}
	f := a.fixer(s)
	a.fx.running = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		_, err := f.Undo(ctx, u)
		mygo.RunOnMain(func() {
			a.fx.running = false
			if err != nil {
				a.toastErr = helperError(err)
			} else {
				delete(a.fx.undo, id)
				a.toast = i18n.T.FixUndone
				a.afterFix(nil)
			}
			a.invalidate()
		})
	}()
}

// guidedStep is the step a guided fix needs first: its place on 订阅与覆写
// is empty until the helper is installed and the config taken over. nil:
// go there now.
func (a *app) guidedStep(rf rowFix, s status.Snapshot) *fixAction {
	if guidedPage(rf.spec.Target) != pageConfig || a.fx.shotHelper != nil {
		return nil
	}
	T := i18n.T
	switch {
	case a.wizardPending(s) && !s.TakenOver():
		return &fixAction{label: T.WizResume, run: a.openWizard}
	case s.Helper.State == status.HelperNotInstalled:
		return &fixAction{label: T.FixInstallHelper, run: func() { a.openPage(pageInstall) }, off: !s.WritesAllowed}
	case !s.TakenOver():
		return &fixAction{label: T.FixTakeOver, run: func() { a.openPage(pageTakeover) }, off: !s.WritesAllowed}
	}
	return nil
}

// guidedPage is the page a guided fix's target is on ("" for none).
func guidedPage(target string) string {
	switch target {
	case health.TargetOverrides, health.TargetLeakGuard:
		return pageConfig
	case health.TargetProxies:
		return pageProxies
	}
	return ""
}

// goFix takes the user to where a guided fix is made, rings the control
// for a moment and says the one thing to change.
func (a *app) goFix(rf rowFix, param string) {
	spot := ""
	switch rf.spec.Target {
	case health.TargetOverrides:
		a.openPage(pageConfig)
	case health.TargetLeakGuard:
		a.hd.cfg.sel, a.hd.cfg.isNew = "leak-guard", false
		spot = "layer:leak-guard"
		a.openPage(pageConfig)
	case health.TargetProxies:
		if param != "" {
			a.groupsOpen[param] = true
			spot = "group:" + param
		}
		a.openPage(pageProxies)
	}
	a.toastHint = rf.text.How
	if spot != "" {
		a.fx.spot, a.fx.spotTill = spot, time.Now().Add(4*time.Second)
		go func() {
			time.Sleep(4100 * time.Millisecond)
			mygo.RunOnMain(a.invalidate)
		}()
	}
}

// spotRing rings e while it is the guided fix's control.
func (a *app) spotRing(c *ui.Context, e ui.Element, key string) {
	if a.fx.spot == key && time.Now().Before(a.fx.spotTill) {
		e.Border(2, c.Theme().Accent).Radius(plat.innerRadius)
	}
}

// systemSettingsURL opens a manual fix's system setting on this
// platform; "" where there is no such link.
func systemSettingsURL(open, platform string) string {
	if platform != runtime.GOOS {
		return "" // a report from another machine: nothing to open here
	}
	switch platform + "|" + open {
	case "darwin|" + health.OpenVPNSettings:
		return "x-apple.systempreferences:com.apple.NetworkExtensionSettingsUI.NESettingsUIExtension"
	case "darwin|" + health.OpenNetworkSettings:
		return "x-apple.systempreferences:com.apple.Network-Settings.extension"
	case "windows|" + health.OpenVPNSettings:
		return "ms-settings:network-vpn"
	case "windows|" + health.OpenNetworkSettings:
		return "ms-settings:network"
	}
	return ""
}

// rowFixLine is the collapsed row's text: the first reason in plain
// words, else the check's summary.
func rowFixLine(cr health.CheckReport, platform string) string {
	if fs := rowFixes(cr, platform); len(fs) > 0 {
		return fs[0].text.Plain
	}
	return cr.Summary
}

// reasonFixes draws an opened row's reasons and their actions.
func (a *app) reasonFixes(c *ui.Context, cr health.CheckReport, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	platform := reportPlatform(s)
	for _, rf := range rowFixes(cr, platform) {
		ui.Column(c.Key("fix:" + rf.reason)).Gap(6).Children(func() {
			ui.Text(c, rf.text.Plain).Wrap().LineHeight(lineText).Selectable()
			switch rf.spec.Kind {
			case health.FixAuto:
				if rf.spec.Action == health.FixEgressProbe && cr.ID == (health.LeakEgress{}).ID() {
					break // the row's own 重新检测 does it
				}
				// What it will do shows in the sheet; the row stays short.
				if act := a.fixButton(cr, rf, s); act != nil {
					busy := a.fx.loading == cr.ID || rf.spec.Action == health.FixEgressProbe && a.egress.running ||
						rf.spec.Action == health.FixRerun && s.Health.Running
					ui.Row(c).Children(func() {
						if busyButton(c.Key("fix-do"), act.label, false, busy, act.off).Clicked() {
							act.run()
						}
					})
				}
			case health.FixGuided:
				ui.Row(c).Children(func() {
					if step := a.guidedStep(rf, s); step != nil {
						// The place is empty until the step before it is done.
						if pushButton(c.Key("fix-step"), step.label).Disabled(step.off).Clicked() {
							step.run()
						}
						return
					}
					if pushButton(c.Key("fix-go"), T.FixGo).Tooltip(rf.text.How).Clicked() {
						a.goFix(rf, fixParam(cr))
					}
				})
			case health.FixManual:
				key := cr.ID + "|" + rf.reason
				open := a.fx.why[key]
				if linkDisclosure(c.Key("fix-why"), open, T.FixWhyNot).Clicked() {
					if a.fx.why == nil {
						a.fx.why = map[string]bool{}
					}
					a.fx.why[key] = !open
				}
				if !open {
					break
				}
				ui.Column(c).Gap(8).MarginStart(16).Children(func() {
					ui.Text(c, rf.text.How).TextColor(t.TextMuted).Wrap().LineHeight(lineText).Selectable()
					if cr.Fix != nil && cr.Fix.Command != "" {
						commandBox(c, cr.Fix.Command)
					}
					ui.Row(c).Gap(8).Children(func() {
						if u := systemSettingsURL(rf.spec.Open, platform); u != "" && pushButton(c.Key("fix-open"), T.FixOpenSystem).Clicked() {
							_ = mygo.Shell.OpenExternal(u)
						}
						if rf.spec.Action == health.FixRerun && pushButton(c.Key("fix-rerun"), T.FixRerun).Disabled(s.Health.Running).Clicked() {
							a.rerunHealth()
						}
					})
				})
			}
		})
	}
	if a.fx.undo[cr.ID] != nil {
		ui.Row(c).Children(func() {
			if busyButton(c.Key("fix-undo"), T.FixUndo, false, a.fx.running, a.fx.running).Clicked() {
				a.undoFix(cr.ID, s)
			}
		})
	}
}

func fixParam(cr health.CheckReport) string {
	if cr.Fix != nil {
		return cr.Fix.Param
	}
	return ""
}

// commandBox is a command to copy.
func commandBox(c *ui.Context, cmd string) {
	p := pal(c.Theme())
	T := i18n.T
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "$ "+strings.ReplaceAll(cmd, "\n", "\n$ ")).Font("monospace").FontSize(plat.calloutSize).
			Padding(8, 10).Radius(plat.innerRadius).Background(p.code).Border(1, p.groupLine).Grow(1).Wrap().LineHeight(lineText).Selectable()
		if iconButton(c.Key("copy-cmd"), iconCopy, T.CopyCmd, false).Clicked() {
			copyText(c, cmd)
		}
	})
}

// fixSheet is the preview of a one-click fix: what is wrong, what the fix
// will do, whether it can be undone, then 修复.
func (a *app) fixSheet(c *ui.Context, s status.Snapshot) {
	p := a.fx.plan
	if p == nil {
		return
	}
	open := true
	ui.Modal(c.Key("fix-sheet"), &open, func() {
		t := c.Theme()
		T := i18n.T
		ui.Column(c).Gap(12).Width(460).MaxWidth(540).Children(func() {
			ui.Text(c, fmt.Sprintf(T.FixSheetTitle, a.fx.title)).FontSize(plat.headingSize).FontWeight(plat.headingWeight)
			ui.Text(c, p.Text.Plain).Wrap().LineHeight(lineText)
			ui.Column(c).Gap(6).Children(func() {
				small(c, T.FixWillDo)
				for _, st := range p.Steps {
					ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
						ui.Text(c, "•").TextColor(t.TextMuted)
						ui.Text(c, st).Grow(1).Shrink(1).Wrap().LineHeight(lineText)
					})
				}
			})
			undo := T.FixNoUndo
			if p.Undo {
				undo = T.FixUndoable
			}
			small(c, undo).Wrap().LineHeight(lineText)
			if p.Blocked != "" {
				errorNote(c, p.Blocked)
			}
			if a.fx.err != "" {
				errorNote(c, a.fx.err)
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.End).Children(func() {
				if a.fx.running && configAction(p.Action) {
					// The apply's stage, as 应用 shows it (save-perf).
					small(c, stageText(a.hd.applyStage)).SingleLine().Shrink(1)
				}
				ui.Spacer(c)
				if pushButton(c.Key("fix-cancel"), T.Cancel).Disabled(a.fx.running).Clicked() {
					a.fx.plan, a.fx.err = nil, ""
				}
				label := T.FixApply
				if a.fx.running {
					label = T.FixRunning
				}
				if busyButton(c.Key("fix-apply"), label, true, a.fx.running, a.fx.running || p.Blocked != "").Clicked() {
					a.runFix(s)
				}
			})
		})
	})
	if !open && !a.fx.running {
		a.fx.plan, a.fx.err = nil, ""
	}
}

// firstAutoFix is the first problem check with a one-click fix that runs
// as it is (no step before it), for the flyout's health row.
func (a *app) firstAutoFix(s status.Snapshot) (health.CheckReport, rowFix, bool) {
	rep := s.Health.Report
	if rep == nil {
		return health.CheckReport{}, rowFix{}, false
	}
	for _, cr := range rep.Checks {
		if cr.Status != health.StatusWarn && cr.Status != health.StatusFail {
			continue
		}
		for _, rf := range rowFixes(cr, reportPlatform(s)) {
			if rf.spec.Kind == health.FixAuto {
				return cr, rf, true
			}
		}
	}
	return health.CheckReport{}, rowFix{}, false
}
