package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/setup"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// The first-run wizard: the whole window, no sidebar, six steps from a
// fresh install to a working proxy and (optionally) tailnet. It opens by
// itself on the first launch of a machine that is not set up
// (wizardLaunchCheck), resumes at the step it was left on, and 稍后再说
// leaves a 继续设置 entry on 总览, on the helper pages and in the flyout.
//
//	① 欢迎       what Twinnel is and what the wizard will do
//	② 后台助手   setup.StepHelper (one administrator prompt)
//	③ 代理内核   take over a running mihomo (the 接管 page), or
//	             setup.StepMihomo (download the official one)
//	④ 订阅       setup.StepSubscription, then config, apply and DNS; the
//	             running config's nodes and rules are counted
//	⑤ Tailscale  setup.StepTailscale (tailscale.install), then the login
//	             (tailscaleLoginView, else setup.StepLogin); skippable
//	⑥ 完成       what is set up, the health checks, where the icon lives
//
// Every write runs through core/setup's Flow (a.setupFlow, setuppage.go);
// the wizard decides only which steps run when.

const pageWizard = "wizard"

// wizMaxW is the wizard's column: narrower than a page, for reading.
const wizMaxW = 600

// tailscaleLoginView draws Tailscale's login inside the wizard's ⑤ when
// set (feat/ts-login-ui owns the component; its file sets this from an
// init) and reports whether the node is logged in. nil: the wizard's own
// fallback, setup.StepLogin through the helper (open the browser, wait).
var tailscaleLoginView func(a *app, c *ui.Context, s status.Snapshot) (loggedIn bool)

// wizardView is the wizard's state in this run.
type wizardView struct {
	state   wizardState
	path    string // setup.json; "" in shot mode
	checked bool   // the launch check ran (wizardLaunchCheck)
	// detectedHelper: the flow's steps were detected with the helper
	// connected (before it was, nothing could be).
	detectedHelper bool

	planOpen bool // ② 会做哪些改动
	ownMih   bool // ③ 不接管，下载新的
	viaSub   bool // ③ failed: download through the subscription once it is added

	// ④ the counted config.
	counting     bool
	counted      bool
	nodes, rules int
	importName   string
	importData   []byte
	// subKernel: ④ downloads the kernel too (③ left it for the
	// subscription's nodes), so its progress lists it.
	subKernel bool

	// goos picks the platform wording (WizardOSFor); a shot run sets
	// it to show another system's words.
	goos string
	// shot fixes the wizard's facts a shot cannot get from a flow.
	shot *wizardShot
}

// wizardShot is a shot's facts (shots_wizard.go).
type wizardShot struct {
	tsInstalled bool
	tsVersion   string
	tsLoggedIn  bool
	tailnet     string
	mihomoVer   string
	takenOver   bool
}

func (a *app) wizOS() i18n.WizardOS {
	g := a.wiz.goos
	if g == "" {
		g = runtime.GOOS
	}
	return i18n.WizardOSFor(g)
}

// openWizard opens the main window on the wizard, at the step it was left
// on. Main thread.
func (a *app) openWizard() {
	if a.wiz.state.Later {
		a.wiz.state.Later = false
		a.saveWizard()
	}
	a.openPage(pageWizard)
}

// leaveWizard is 稍后再说: back to 总览, the wizard no longer opens by
// itself, and 继续设置 stays where the user will see it.
func (a *app) leaveWizard() {
	a.wiz.state.Later = true
	a.saveWizard()
	a.showPage(pageOverview)
}

// finishWizard is 完成 on the last step.
func (a *app) finishWizard() {
	a.wiz.state.Done, a.wiz.state.Later = true, false
	a.saveWizard()
	a.showPage(pageOverview)
}

// wizardWindow is the whole window while the wizard shows.
func (a *app) wizardWindow(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	w := &a.wiz
	if s.Helper.Connected() && !w.detectedHelper && !a.setupV.running {
		// The helper came up (② installed it): find what is done again.
		w.detectedHelper = true
		a.setupV.detected = false
	}
	a.setupDetect()
	barH := toolbarHeight(c)
	ui.Column(c.Key("wizard")).Fill().Background(t.Background).Children(func() {
		// The title bar's band: drags the window, clears the traffic
		// lights, says where the user is.
		ui.Row(c).Height(barH).FillWidth().Shrink(0).DragWindow().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			displayFont(ui.Text(c, T.WizTitle).FontSize(plat.titleBarSize).FontWeight(plat.titleBarWeight).SingleLine())
		})
		ui.Scroll(c.Key("wiz-scroll:" + fmt.Sprint(w.state.Step))).Grow(1).Children(func() {
			// The column is centered in the window at every width.
			ui.Row(c).FillWidth().Justify(ui.Center).Padding(8, plat.pagePadX, 24).Children(func() {
				ui.Column(c).Gap(gapGroup).FillWidth().MaxWidth(wizMaxW).Children(func() {
					a.wizardProgress(c)
					switch w.state.Step {
					case wizWelcome:
						a.wizWelcome(c)
					case wizHelper:
						a.wizHelper(c, s)
					case wizMihomo:
						a.wizMihomo(c, s)
					case wizSub:
						a.wizSub(c, s)
					case wizTS:
						a.wizTS(c, s)
					case wizDone:
						a.wizDone(c, s)
					}
				})
			})
		})
		a.wizardFooter(c, s)
	})
}

// wizardProgress is the row of six steps: done ones ticked, the current
// one in the accent, with 第 n 步，共 6 步 under it. A step already
// reached can be clicked to go back to it.
func (a *app) wizardProgress(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	cur := a.wiz.state.Step
	ui.Column(c).Gap(8).FillWidth().Children(func() {
		ui.Row(c).Gap(4).FillWidth().AlignItems(ui.Center).Role(ui.RoleList).Children(func() {
			for i, name := range T.WizStepNames {
				done := i < cur
				col := t.TextMuted
				if i == cur {
					col = t.Accent
				}
				item := ui.Column(c.Key(fmt.Sprint("wp", i))).Gap(6).Grow(1).Basis(0).AlignItems(ui.Center).Label(name)
				item.Children(func() {
					bar := ui.Box(c).Height(3).FillWidth().Radius(2)
					switch {
					case i == cur:
						bar.Background(t.Accent)
					case done:
						bar.Background(t.Accent.Alpha(0.45))
					default:
						bar.Background(pal(t).separator)
					}
					ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
						if done {
							ui.Icon(c, iconOK).FontSize(iconSmall).TextColor(pal(t).okText)
						}
						txt := ui.Text(c, name).FontSize(plat.smallSize).TextColor(col).SingleLine()
						if i == cur {
							txt.FontWeight(600)
						}
					})
				})
				if done && item.Role(ui.RoleButton).Clicked() {
					a.setWizardStep(i)
				}
			}
		})
		num(small(c, i18n.F(T.WizProgress, cur+1, wizSteps)))
	})
}

// wizardFooter: 稍后再说 on the left, 上一步 and the step's way on at the
// right. The way on is the accent once the step is done; until then the
// step's own button is.
func (a *app) wizardFooter(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	step := a.wiz.state.Step
	ui.Box(c).Height(1).FillWidth().Background(pal(t).separator).Shrink(0)
	ui.Row(c).FillWidth().Padding(12, plat.pagePadX).Shrink(0).Justify(ui.Center).Children(func() {
		ui.Row(c).Gap(10).FillWidth().MaxWidth(wizMaxW).AlignItems(ui.Center).Children(func() {
			if step != wizDone {
				if pushButton(c.Key("later"), T.WizLater).Clicked() {
					a.leaveWizard()
				}
			}
			ui.Spacer(c)
			if step > wizWelcome {
				if pushButton(c.Key("back"), T.WizBack).Disabled(a.setupV.running).Clicked() {
					a.setWizardStep(step - 1)
				}
			}
			switch step {
			case wizWelcome:
				if ui.PrimaryButton(c.Key("start"), T.WizStart).Clicked() {
					a.setWizardStep(wizHelper)
				}
			case wizTS:
				if a.wizTSDone(s) {
					if ui.PrimaryButton(c.Key("next"), T.WizNext).Clicked() {
						a.setWizardStep(wizDone)
					}
				} else if pushButton(c.Key("skip"), T.WizSkip).Disabled(a.setupV.running).Clicked() {
					a.wiz.state.TSSkipped = true
					a.setWizardStep(wizDone)
				}
			case wizDone:
				if ui.PrimaryButton(c.Key("finish"), T.WizFinish).Clicked() {
					a.finishWizard()
				}
			default:
				done := a.wizStepDone(step, s)
				if b := primaryButton(c.Key("next"), T.WizNext, !done || a.setupV.running); b.Clicked() {
					a.setWizardStep(step + 1)
				}
			}
		})
	})
}

// wizStepDone reports whether a step needs nothing more.
func (a *app) wizStepDone(step int, s status.Snapshot) bool {
	switch step {
	case wizHelper:
		return a.wizHelperDone(s)
	case wizMihomo:
		return a.wizMihomoDone(s)
	case wizSub:
		return a.wizSubDone(s)
	case wizTS:
		return a.wizTSDone(s) || a.wiz.state.TSSkipped
	}
	return true
}

// wizStep is one of the flow's steps (or a shot's).
func (a *app) wizStep(id setup.Step) setup.StepState {
	for _, st := range a.setupSteps() {
		if st.ID == id {
			return st
		}
	}
	return setup.StepState{ID: id, State: setup.Pending}
}

func isDone(st setup.StepState) bool { return st.State == setup.Done || st.State == setup.Skipped }

// stepHead is a step's heading and the paragraph that says what it does
// and why.
func stepHead(c *ui.Context, title, body string) {
	ui.Column(c).Gap(8).FillWidth().Children(func() {
		displayFont(ui.Text(c, title).FontSize(plat.titleSize - 3).FontWeight(plat.titleWeight))
		ui.Text(c, body).Wrap().LineHeight(lineText)
	})
}

// stepState is a step's outcome line: ✓ and what was done, a spinner and
// what is running, or nothing.
func stepState(c *ui.Context, icon *ui.SVG, col ui.Color, text string) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Children(func() {
		if icon == nil {
			spinnerIcon(c)
		} else {
			ui.Icon(c, icon).FontSize(iconInline).TextColor(col).Shrink(0)
		}
		ui.Text(c, text).Wrap().LineHeight(lineText).Grow(1).Shrink(1)
	})
}

func (a *app) okLine(c *ui.Context, text string) {
	stepState(c, iconOK, pal(c.Theme()).okText, text)
}

// needsHelperNote stands in for a step's action while the helper is not
// connected: why, and the way back to ②.
func (a *app) needsHelperNote(c *ui.Context) {
	T := i18n.T
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconSkip).FontSize(iconInline).TextColor(c.Theme().TextMuted)
		ui.Text(c, T.WizNeedsHelper).Grow(1)
		if pushButton(c.Key("to-helper"), T.WizHelperHead).Clicked() {
			a.setWizardStep(wizHelper)
		}
	})
}

// writesLock is the read-only switch's lock beside a disabled action.
func writesLock(c *ui.Context, s status.Snapshot) {
	if !s.WritesAllowed {
		ui.Icon(c, iconSecure).FontSize(iconInline).TextColor(c.Theme().TextMuted).Tooltip(i18n.T.InstallNeedsWrites)
	}
}

// wizardGate stands in for a helper page while no helper is installed:
// the page needs it, and the wizard is where it gets installed.
func (a *app) wizardGate(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	groupBox(c, t).Padding(12, groupPadX).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(32).Children(func() {
			ui.Icon(c, iconTwinnel).FontSize(iconInline + 2).TextColor(t.Accent).Shrink(0)
			ui.Text(c, T.WizGateHead).FontWeight(600).Grow(1).Shrink(1).Wrap().LineHeight(lineText)
			if ui.PrimaryButton(c.Key("wiz-gate"), T.WizResume).Clicked() {
				a.openWizard()
			}
		})
	})
}

// --- ① 欢迎 ---

func (a *app) wizWelcome(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconTwinnel).FontSize(iconHero + 12).TextColor(t.Accent)
		displayFont(ui.Text(c, T.WizWelcomeHead).FontSize(plat.titleSize).FontWeight(plat.titleWeight))
	})
	ui.Text(c, i18n.F(T.WizWelcomeBody, a.wizOS().Machine)).Wrap().LineHeight(lineText)
	cardWith(c, T.WizPlanHead, nil, func() {
		ui.Column(c).Gap(10).Children(func() {
			for i, line := range T.WizPlan {
				ui.Row(c.Key(fmt.Sprint("plan", i))).Gap(10).AlignItems(ui.Start).Children(func() {
					num(ui.Text(c, fmt.Sprint(i+2)).FontSize(plat.smallSize).FontWeight(600).TextColor(t.Accent).Width(14).TextAlign(ui.End).MarginY(1).Shrink(0))
					ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
						ui.Text(c, T.WizStepNames[i+1]).FontWeight(600)
						ui.Text(c, line).TextColor(t.TextMuted).Wrap().LineHeight(lineText)
					})
				})
			}
		})
	})
}

// --- ② 后台助手 ---

func (a *app) wizHelperDone(s status.Snapshot) bool {
	return s.Helper.Connected() || a.wizStep(setup.StepHelper).State == setup.Done
}

func (a *app) wizHelper(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	stepHead(c, T.WizHelperHead, T.WizHelperBody)
	st := a.wizStep(setup.StepHelper)
	running := a.setupV.running && st.State == setup.Running
	groupBox(c, c.Theme()).Gap(12).Children(func() {
		switch {
		case a.wizHelperDone(s):
			a.okLine(c, T.WizHelperDone)
			return
		case st.State == setup.Failed:
			failedRowWhy(c.Key("failed"), T.WizHelperFailed, helperFailure(st.Error), st.Error, func() { a.wizRun(nil, setup.StepHelper) }, a.setupV.running)
			return
		}
		ui.Text(c, a.wizOS().AdminPrompt).Wrap().LineHeight(lineText)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			label := T.WizHelperInstall
			if running {
				label = T.WizHelperInstalling
			}
			b := busyButton(c.Key("install"), label, true, running, a.setupV.running || !s.WritesAllowed)
			disabledPrimaryFace(c, b)
			if b.Clicked() {
				a.wizRun(nil, setup.StepHelper)
			}
			writesLock(c, s)
		})
	})
	if a.wizHelperDone(s) {
		return
	}
	// What installing changes: the CLI's dry run, in words (onboarding.go).
	if linkDisclosure(c.Key("changes"), a.wiz.planOpen, T.WizHelperChanges).Clicked() {
		a.wiz.planOpen = !a.wiz.planOpen
		if a.wiz.planOpen && !a.hd.onb.loaded {
			a.loadInstallPlan()
		}
	}
	if a.wiz.planOpen {
		a.hd.onb.stepsOpen = true
		a.installPlanList(c)
	}
}

// helperFailure words a failed install: a closed administrator prompt
// (every platform's CLI says "cancelled; nothing was changed") in the UI
// language, else the CLI's own "twinnel: …" line; the raw output stays
// behind 复制详情.
func helperFailure(raw string) string {
	line := cliFailure(raw, nil)
	if strings.Contains(strings.ToLower(line), "cancelled") {
		return i18n.T.WizHelperCancelled
	}
	if plain := i18n.PlainNetError(line); plain != "" {
		return plain
	}
	return line
}

// --- ③ 代理内核 ---

// wizMihomoDone: a kernel is installed (or the helper runs an existing
// one), or Twinnel took over the running mihomo.
func (a *app) wizMihomoDone(s status.Snapshot) bool {
	if a.wizTakenOver(s) {
		return true
	}
	return isDone(a.wizStep(setup.StepMihomo))
}

func (a *app) wizTakenOver(s status.Snapshot) bool {
	if sh := a.wiz.shot; sh != nil {
		return sh.takenOver
	}
	return s.Helper.Connected() && s.TakenOver() && a.live.found()
}

func (a *app) wizMihomo(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	stepHead(c, T.WizMihomoHead, T.WizMihomoBody)
	if !a.live.loaded {
		a.detectLiveSetup()
	}
	st := a.wizStep(setup.StepMihomo)
	groupBox(c, c.Theme()).Gap(12).Children(func() {
		switch {
		case a.wizTakenOver(s):
			a.okLine(c, T.WizMihomoTakenOver)
			return
		case isDone(st):
			if st.Detail == "" {
				a.okLine(c, T.WizMihomoExternal)
			} else {
				a.okLine(c, i18n.F(T.WizMihomoDone, st.Detail))
			}
			for i, w := range st.Warnings {
				small(c.Key(fmt.Sprint("warn", i)), setupWarning(w)).Wrap().LineHeight(lineText).TextColor(pal(c.Theme()).warnText)
			}
			return
		case !s.Helper.Connected() && !a.wizHelperDone(s):
			a.needsHelperNote(c)
			return
		case a.wiz.viaSub:
			stepState(c, iconSkip, c.Theme().TextMuted, T.WizSubDownloadLater)
			return
		case a.live.found() && !a.wiz.ownMih && st.State != setup.Failed && st.State != setup.Running:
			a.wizTakeOverChoice(c, s)
			return
		case st.State == setup.Failed:
			failedRowWhy(c.Key("failed"), T.WizMihomoFailed, setupReason(st), st.Error, func() { a.wizRun(nil, setup.StepMihomo) }, a.setupV.running)
			if st.Code == setup.CodeDownload {
				a.wizDownloadChoices(c, false)
			}
			return
		}
		running := a.setupV.running && st.State == setup.Running
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			label := T.WizMihomoDownload
			if running {
				label = T.WizMihomoDownloading
			}
			b := busyButton(c.Key("download"), label, true, running, a.setupV.running || !s.WritesAllowed)
			disabledPrimaryFace(c, b)
			if b.Clicked() {
				a.setupFlow().Opts.Proxy = ""
				a.wizRun(nil, setup.StepMihomo)
			}
			writesLock(c, s)
		})
	})
}

// wizTakeOverChoice: a mihomo already runs here. Taking it over keeps the
// person's setup (the 接管 page does it, with its preview); downloading a
// new one starts from a subscription.
func (a *app) wizTakeOverChoice(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	ui.Text(c, i18n.F(T.WizMihomoFound, tildePath(a.live.Config))).Wrap().LineHeight(lineText)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
		if primaryButton(c.Key("takeover"), T.WizMihomoTakeOver, !s.WritesAllowed).Clicked() {
			a.openPage(pageTakeover)
		}
		infoTipAbout(c, T.WizMihomoTakeOver, T.TipWizTakeOver, nil)
		ui.Spacer(c)
		if pushButton(c.Key("own"), T.WizMihomoOwn).Clicked() {
			a.wiz.ownMih = true
		}
	})
}

// wizDownloadChoices are a failed download's other ways: through the
// subscription's nodes (from ③: add it first, in ④), or a proxy address.
// inSub: the failed row is in ④'s progress, where the subscription was
// already tried.
func (a *app) wizDownloadChoices(c *ui.Context, inSub bool) {
	T := i18n.T
	v := &a.setupV
	ui.Column(c).Gap(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			if !inSub && pushButton(c.Key("via-sub"), T.WizMihomoSubFirst).Disabled(v.running).Clicked() {
				a.wiz.viaSub = true
				a.setWizardStep(wizSub)
			}
			if pushButton(c.Key("via-proxy"), T.SetupViaProxy).Disabled(v.running).Clicked() {
				v.proxyOpen = !v.proxyOpen
			}
			infoTipAbout(c, T.SetupViaSub, T.TipSetupProxy, nil)
		})
		if !v.proxyOpen {
			return
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("proxy"), &v.proxyAddr).Label(T.SetupProxyAddr).Placeholder("http://192.168.1.2:7890").Grow(1)
			if pushButton(c.Key("proxy-use"), T.SetupProxyUse).Disabled(v.running || v.proxyAddr == "").Clicked() {
				a.setupFlow().Opts.Proxy = v.proxyAddr
				v.proxyOpen = false
				a.wizRetry(setup.StepMihomo)
			}
		})
	})
}

// --- ④ 订阅 ---

// wizSubSteps are what ④ runs after the subscription: the kernel when
// ③ left it for the subscription's nodes, the config, the start, the DNS.
var wizSubSteps = []setup.Step{setup.StepSubscription, setup.StepMihomo, setup.StepConfig, setup.StepApply, setup.StepDNS}

// wizSubDone: the proxy runs on a subscription (set up here, or taken
// over).
func (a *app) wizSubDone(s status.Snapshot) bool {
	if a.wizTakenOver(s) {
		return true
	}
	return isDone(a.wizStep(setup.StepSubscription)) && isDone(a.wizStep(setup.StepApply))
}

func (a *app) wizSub(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	t := c.Theme()
	v := &a.setupV
	w := &a.wiz
	stepHead(c, T.WizSubHead, T.WizSubBody)
	done := a.wizSubDone(s)
	if done && !w.counted && !w.counting {
		a.countConfig()
	}
	groupBox(c, t).Gap(12).Children(func() {
		switch {
		case done:
			switch {
			case w.counting || !w.counted:
				stepState(c, nil, t.TextMuted, T.WizSubCounting)
			case w.nodes < 0:
				a.okLine(c, T.WizSubRunning)
			case w.nodes == 0:
				stepState(c, iconWarning, t.Warning, T.WizSubNoNodes)
			default:
				a.okLine(c, i18n.F(T.WizSubReady, i18n.Count(w.nodes, T.WizNode, T.WizNodes), i18n.Count(w.rules, T.SumRule, T.SumRules)))
			}
			return
		case !s.Helper.Connected() && !a.wizHelperDone(s):
			a.needsHelperNote(c)
			return
		}
		ui.TextInput(c.Key("sub-url"), &v.url).Label(T.WizSubURL).Placeholder(T.WizSubPlaceholder).FillWidth().Disabled(v.running)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			if w.importName != "" {
				ui.Icon(c, iconOK).FontSize(iconInline).TextColor(pal(t).okText)
				ui.Text(c, i18n.F(T.WizSubImported, w.importName)).SingleLine().Shrink(1)
				if actionLink(c.Key("clear"), T.WizSubClear).Clicked() {
					w.importName, w.importData = "", nil
				}
			} else if setupImport != nil {
				if pushButton(c.Key("import"), T.WizSubImport).Disabled(v.running).Clicked() {
					a.pickWizardImport()
				}
				infoTipAbout(c, T.WizSubImport, T.TipWizImport, nil)
			}
		})
		if sub := a.wizStep(setup.StepSubscription); sub.State == setup.Waiting && w.importName != "" {
			stepState(c, iconWarning, t.Warning, T.WizSubNeedsURL)
		}
		if strings.HasSuffix(strings.ToLower(w.importName), ".zip") {
			stepState(c, iconWarning, t.Warning, T.SetupImportSecret)
		}
		running := v.running
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			label := T.WizSubStart
			if running {
				label = T.WizSubStarting
			}
			need := strings.TrimSpace(v.url) == "" && w.importData == nil && !isDone(a.wizStep(setup.StepSubscription))
			b := busyButton(c.Key("start"), label, true, running, running || need || !s.WritesAllowed)
			disabledPrimaryFace(c, b)
			if b.Clicked() {
				a.wizStartProxy()
			}
			writesLock(c, s)
		})
	})
	if a.wizSubStarted() {
		ui.Column(c).Gap(8).Children(func() {
			groupHead(c).Children(func() {
				ui.Text(c, T.WizSubProgressHead).FontSize(plat.headingSize).FontWeight(plat.headingWeight)
			})
			listBox(c, t).Children(func() {
				n := 0
				for _, id := range wizSubSteps {
					if id == setup.StepMihomo && !w.subKernel {
						continue // ③ installed it
					}
					n++
					a.setupStepRow(c.Key(fmt.Sprint("sub-", id)), n, a.wizStep(id))
				}
			})
		})
	}
}

// wizSubStarted: one of ④'s steps ran (so its progress list shows).
func (a *app) wizSubStarted() bool {
	for _, id := range wizSubSteps[1:] {
		if st := a.wizStep(id); st.State == setup.Running || st.State == setup.Failed {
			return true
		}
	}
	st := a.wizStep(setup.StepSubscription)
	return st.State == setup.Running || st.State == setup.Failed || st.State == setup.Waiting
}

// wizStartProxy runs ④: the subscription (the pasted link, the picked
// file, or both), then whatever of the kernel, config, start and DNS is
// not done.
func (a *app) wizStartProxy() {
	v := &a.setupV
	w := &a.wiz
	f := a.setupFlow()
	f.Opts.SubscriptionURL = strings.TrimSpace(v.url)
	f.Opts.ImportZip = w.importData
	if setupImport != nil && w.importName != "" {
		f.Env.ImportConfig = setup.ImportConfigNamed(w.importName)
	}
	if w.viaSub {
		f.Opts.Proxy = rpc.ProxySubscription
	}
	steps := []setup.Step{setup.StepSubscription}
	w.subKernel = !isDone(a.wizStep(setup.StepMihomo))
	for _, id := range wizSubSteps[1:] {
		if !isDone(a.wizStep(id)) {
			steps = append(steps, id)
		}
	}
	w.counted = false
	a.wizRun(func() { w.viaSub = false }, steps...)
}

// pickWizardImport reads a file the person picks: an export zip, or a
// Clash Verge / Clash Party config or override.
func (a *app) pickWizardImport() {
	parent := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: i18n.Cur().WizSubImport,
			Filters: []mygo.FileFilter{{Name: i18n.Cur().ImportFiles, Extensions: []string{"zip", "yaml", "yml", "txt", "conf", "list"}}}})
		if err != nil || len(paths) == 0 {
			return
		}
		b, err := readSetupZip(paths[0])
		mygo.RunOnMain(func() {
			if err != nil {
				a.toastErr = err.Error()
			} else {
				a.wiz.importData, a.wiz.importName = b, baseName(paths[0])
			}
			a.invalidate()
		})
	}()
}

// countConfig reads the running config's nodes and rules from mihomo's
// controller (with the helper's credentials), retrying while mihomo
// starts.
func (a *app) countConfig() {
	w := &a.wiz
	if a.shotMode || w.counting {
		return
	}
	w.counting = true
	hc := a.hc
	cfg := a.cfg
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		nodes, rules, ok := 0, 0, false
		for !ok && ctx.Err() == nil {
			var creds *rpc.ControllerCredentials
			if hc != nil {
				creds, _, _ = fetchCredentials(ctx, hc)
			}
			cc := controllerConfig(cfg, creds)
			if mh, err := mihomo.New(mihomo.Options{Address: cc.Controller, Secret: cc.Secret}); err == nil {
				ps, perr := mh.Proxies(ctx)
				n, rerr := mh.RuleCount(ctx)
				if perr == nil && rerr == nil {
					nodes, rules, ok = mihomo.CountNodes(ps), n, true
					break
				}
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
		mygo.RunOnMain(func() {
			w.counting, w.counted = false, ok
			w.nodes, w.rules = nodes, rules
			if !ok {
				w.counted = true // shown as running; the count is a nicety
				w.nodes = -1
			}
			a.invalidate()
		})
	}()
}

// wizRetry runs a failed step again; in ④, then the rest of ④.
func (a *app) wizRetry(id setup.Step) {
	steps := []setup.Step{id}
	for i, s := range wizSubSteps {
		if s != id {
			continue
		}
		for _, next := range wizSubSteps[i+1:] {
			if !isDone(a.wizStep(next)) {
				steps = append(steps, next)
			}
		}
	}
	if a.wiz.state.Step != wizSub {
		steps = steps[:1]
	}
	a.wizRun(nil, steps...)
}

// --- ⑤ Tailscale ---

func (a *app) wizTSInstalled(s status.Snapshot) (bool, string) {
	if sh := a.wiz.shot; sh != nil {
		return sh.tsInstalled, sh.tsVersion
	}
	if st := a.wizStep(setup.StepTailscale); st.State == setup.Done {
		return true, st.Detail
	}
	if s.Tailscale.Reachable {
		return true, s.Tailscale.State.Version
	}
	k := s.Tailscale.Install.Kind
	return k != "" && k != tailscale.InstallUnknown, ""
}

func (a *app) wizTSDone(s status.Snapshot) bool {
	if sh := a.wiz.shot; sh != nil {
		return sh.tsLoggedIn
	}
	return s.Tailscale.Reachable && s.Tailscale.State.Running() || a.wizStep(setup.StepLogin).State == setup.Done
}

func (a *app) wizTS(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	t := c.Theme()
	stepHead(c, T.WizTSHead, T.WizTSBody)
	installed, ver := a.wizTSInstalled(s)
	inst := a.wizStep(setup.StepTailscale)
	groupBox(c, t).Gap(12).Children(func() {
		switch {
		case installed:
			if ver != "" {
				a.okLine(c, i18n.F(T.WizTSFound, ver))
			} else {
				a.okLine(c, T.WizTSFoundBare)
			}
			for i, w := range inst.Warnings {
				small(c.Key(fmt.Sprint("warn", i)), setupWarning(w)).Wrap().LineHeight(lineText).TextColor(pal(t).warnText)
			}
			if s.Tailscale.Install.UsesSystemVPN && a.wiz.shot == nil {
				small(c, T.SetupWarnSystemVPN).Wrap().LineHeight(lineText).TextColor(pal(t).warnText)
			}
		case !s.Helper.Connected() && !a.wizHelperDone(s):
			a.needsHelperNote(c)
			return
		case inst.State == setup.Failed:
			failedRowWhy(c.Key("failed"), T.WizTSFailed, setupReason(inst), inst.Error, func() { a.wizRun(nil, setup.StepTailscale) }, a.setupV.running)
			return
		default:
			ui.Text(c, a.wizOS().TSInstallHow).Wrap().LineHeight(lineText)
			running := a.setupV.running && inst.State == setup.Running
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				label := T.WizTSInstall
				if running {
					label = T.WizTSInstalling
				}
				b := busyButton(c.Key("ts-install"), label, true, running, a.setupV.running || !s.WritesAllowed)
				disabledPrimaryFace(c, b)
				if b.Clicked() {
					a.wizRun(nil, setup.StepTailscale)
				}
				writesLock(c, s)
			})
			return
		}
	})
	if !installed {
		return
	}
	groupBox(c, t).Gap(12).Children(func() {
		if tailscaleLoginView != nil && a.wiz.shot == nil {
			tailscaleLoginView(a, c, s)
			return
		}
		a.wizTSLoginFallback(c, s)
	})
}

// wizTSLoginFallback is the login without feat/ts-login-ui's component:
// setup.StepLogin through the helper opens the login page in the browser
// and waits until the node runs.
func (a *app) wizTSLoginFallback(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	t := c.Theme()
	st := a.wizStep(setup.StepLogin)
	if a.wizTSDone(s) {
		tailnet := st.Detail
		if sh := a.wiz.shot; sh != nil {
			tailnet = sh.tailnet
		} else if tailnet == "" && s.Tailscale.Reachable {
			tailnet = s.Tailscale.State.Tailnet
		}
		if tailnet != "" {
			a.okLine(c, i18n.F(T.WizTSLoggedIn, tailnet))
		} else {
			a.okLine(c, T.WizTSLoggedInBare)
		}
		return
	}
	switch st.State {
	case setup.Failed:
		failedRowWhy(c.Key("login-failed"), T.WizTSLogin, setupReason(st), st.Error, func() { a.wizRun(nil, setup.StepLogin) }, a.setupV.running)
		return
	case setup.Waiting, setup.Running:
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			spinnerIcon(c)
			ui.Text(c, T.WizTSLoginWait).Grow(1).Shrink(1).Wrap().LineHeight(lineText)
			if st.URL != "" && pushButton(c.Key("login-again"), T.WizTSLoginAgain).Clicked() {
				_ = mygo.Shell.OpenExternal(st.URL)
			}
		})
		return
	}
	ui.Text(c, T.WizTSLoginHint).TextColor(t.TextMuted).Wrap().LineHeight(lineText)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		b := busyButton(c.Key("login"), T.WizTSLogin, true, false, a.setupV.running || !s.WritesAllowed || !s.Helper.Connected())
		disabledPrimaryFace(c, b)
		if b.Clicked() {
			a.wizRun(nil, setup.StepLogin)
		}
		writesLock(c, s)
	})
}

// --- ⑥ 完成 ---

func (a *app) wizDone(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	t := c.Theme()
	warn, fail := s.Health.Counts()
	issues := warn + fail
	head := T.WizDoneHead
	if issues > 0 && s.Health.Report != nil {
		head = i18n.F(T.WizDoneIssues, issues)
	}
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		icon, col := iconOK, pal(t).okText
		if issues > 0 {
			icon, col = iconWarning, t.Warning
		}
		ui.Icon(c, icon).FontSize(iconHero).TextColor(col)
		displayFont(ui.Text(c, head).FontSize(plat.titleSize - 3).FontWeight(plat.titleWeight))
	})
	listBox(c, t).Children(func() {
		a.doneRow(c, "helper", T.WizDoneHelper, a.wizHelperDone(s), T.WizDoneInstalled, wizHelper)
		proxy := T.WizDoneInstalled
		if s.Mihomo.Reachable && s.Mihomo.Version != "" {
			proxy = i18n.F(T.WizDoneRunning, s.Mihomo.Version)
		} else if sh := a.wiz.shot; sh != nil && sh.mihomoVer != "" {
			proxy = i18n.F(T.WizDoneRunning, sh.mihomoVer)
		}
		a.doneRow(c, "proxy", T.WizDoneProxy, a.wizMihomoDone(s), proxy, wizMihomo)
		sub := T.WizDoneInstalled
		if a.wiz.counted && a.wiz.nodes > 0 {
			sub = i18n.Count(a.wiz.nodes, T.WizNode, T.WizNodes)
		}
		a.doneRow(c, "sub", T.WizDoneSub, a.wizSubDone(s), sub, wizSub)
		switch {
		case a.wizTSDone(s):
			a.doneRow(c, "ts", T.WizDoneTS, true, T.WizTSLoggedInBare, wizTS)
		case a.wiz.state.TSSkipped:
			doneLine(c, "ts", func() {
				doneRowHead(c, iconSkip, t.TextMuted, T.WizDoneTS)
				small(c, T.WizDoneSkipped)
				if pushButton(c.Key("ts-fix"), T.WizDoneFix).Clicked() {
					a.wiz.state.TSSkipped = false
					a.setWizardStep(wizTS)
				}
			})
		default:
			a.doneRow(c, "ts", T.WizDoneTS, false, "", wizTS)
		}
		doneLine(c, "health", func() {
			switch {
			case s.Health.Report == nil:
				ui.Box(c).Width(16).AlignItems(ui.Center).Shrink(0).Children(func() { spinnerIcon(c) })
				ui.Text(c, T.WizDoneHealth).Grow(1)
				small(c, T.WizDoneHealthWait)
			case issues == 0:
				doneRowHead(c, iconOK, pal(t).okText, T.WizDoneHealth)
				small(c, T.WizDoneHealthOK)
			default:
				doneRowHead(c, iconWarning, t.Warning, T.WizDoneHealth)
				small(c, i18n.F(T.WizDoneHealthIssues, issues))
				if pushButton(c.Key("health-open"), T.WizDoneHealthOpen).Clicked() {
					a.wiz.state.Done = true
					a.saveWizard()
					a.openPage(pageHealth)
				}
			}
		})
	})
	cardWith(c, T.WizTrayHead, nil, func() {
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Box(c).Padding(6).Radius(6).Background(pal(t).code).Border(1, pal(t).groupLine).Shrink(0).Children(func() {
				ui.Icon(c, iconTwinnel).FontSize(iconLarge).TextColor(t.Text)
			})
			ui.Text(c, a.wizOS().TrayWhere).Wrap().LineHeight(lineText).Grow(1).Shrink(1)
		})
	})
	cardWith(c, T.WizNextHead, nil, func() {
		ui.Column(c).Gap(8).Children(func() {
			for i, line := range T.WizNextSteps {
				ui.Row(c.Key(fmt.Sprint("next", i))).Gap(8).AlignItems(ui.Start).Children(func() {
					ui.Icon(c, iconCollapsed).FontSize(iconInline).TextColor(t.TextMuted).MarginY(1).Shrink(0)
					ui.Text(c, line).Wrap().LineHeight(lineText).Grow(1).Shrink(1)
				})
			}
		})
	})
}

// doneLine is one row of ⑥'s summary: mark, name, detail, action on one
// line.
func doneLine(c *ui.Context, key string, body func()) {
	listRow(c.Key(key)).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).FillWidth().Children(body)
	})
}

// doneRowHead is a summary row's mark and name.
func doneRowHead(c *ui.Context, icon *ui.SVG, col ui.Color, name string) {
	ui.Box(c).Width(16).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Icon(c, icon).FontSize(iconInline).TextColor(col)
	})
	ui.Text(c, name).Grow(1).SingleLine()
}

// doneRow is one line of ⑥'s summary: ✓ and what is there, or what is
// missing with the way back to its step.
func (a *app) doneRow(c *ui.Context, key, name string, done bool, detail string, step int) {
	t := c.Theme()
	T := i18n.T
	doneLine(c, key, func() {
		if done {
			doneRowHead(c, iconOK, pal(t).okText, name)
			num(small(c, detail)).SingleLine()
			return
		}
		doneRowHead(c, iconSkip, t.TextMuted, name)
		small(c, T.WizDoneNotSet)
		if pushButton(c.Key("fix"), T.WizDoneFix).Clicked() {
			a.setWizardStep(step)
		}
	})
}

// --- running steps ---

// wizRun runs the flow's steps in order, stopping at the first that fails
// or waits; then runs after (on the main thread) and refreshes. A step
// already done still runs when it is the first one asked for (a retry).
func (a *app) wizRun(after func(), steps ...setup.Step) {
	v := &a.setupV
	if v.running || a.shotMode || !a.cfg.AllowWrites || len(steps) == 0 {
		return
	}
	f := a.setupFlow()
	v.running = true
	go func() {
		timeout := 30 * time.Minute
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		var err error
		for i, id := range steps {
			if i > 0 && isDone(f.Step(id)) {
				continue
			}
			if err = f.RunStep(ctx, id); err != nil {
				break
			}
		}
		mygo.RunOnMain(func() {
			v.running = false
			if after != nil && err == nil {
				after()
			}
			a.hc.Retry()
			if err == nil && a.wiz.state.Step == wizDone {
				a.rerunHealth()
			}
			a.invalidate()
		})
	}()
}
