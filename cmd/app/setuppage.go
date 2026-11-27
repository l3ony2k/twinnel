package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/setup"
	"github.com/l3ony2k/twinnel/core/status"
)

// The 设置这台 Mac page: the fresh-setup flow of core/setup for a machine
// with no mihomo to take over. It has no sidebar row: 开始使用 on 概览
// opens it (subPages). One row per step with its state; a failed step
// has 重试, a failed download also the proxy choices; the login step
// opens the browser and waits. Everything runs through the helper (the
// first step installs it) and Tailscale's LocalAPI.

const pageSetup = "setup"

func init() { subPages[pageSetup] = pageOverview }

type setupView struct {
	flow     *setup.Flow
	detected bool
	running  bool
	url      string
	// proxyOpen shows the proxy address field of a failed download.
	proxyOpen bool
	proxyAddr string
	// zip is a picked export zip (only offered when setupImport is set).
	zip     []byte
	zipName string
	// shotSteps replaces the flow's steps in shot mode (shots_setup.go).
	shotSteps []setup.StepState
}

// setupImport restores an export zip through the helper's config.import
// (setup.ImportConfig); a test or shot may replace it, and nil hides
// 从备份导入….
var setupImport = setup.ImportConfig

// freshMachine: no helper takeover and no running mihomo was found, so 开始
// 使用 offers 设置这台 Mac instead of 接管现有配置.
func (a *app) freshMachine(s status.Snapshot) bool {
	if s.TakenOver() {
		return false
	}
	if !a.live.loaded {
		a.detectLiveSetup()
		return false
	}
	return !a.live.found()
}

func (a *app) setupFlow() *setup.Flow {
	v := &a.setupV
	if v.flow != nil {
		return v.flow
	}
	hc := a.hc
	v.flow = setup.New(setup.Env{
		Connect: func(ctx context.Context) (setup.Caller, error) {
			var st rpc.Status
			if err := hc.Call(ctx, rpc.MethodStatus, nil, &st); err != nil {
				return nil, err
			}
			return hc, nil
		},
		InstallHelper: func(ctx context.Context) error {
			// The click is the confirmation; macOS asks for the
			// administrator password.
			out, err := a.runCLI(ctx, installArgs("--yes")...)
			hc.Retry()
			if err != nil {
				return errors.New(cliFailure(out, err))
			}
			return nil
		},
		OpenURL:      func(u string) error { return mygo.Shell.OpenExternal(u) },
		Lang:         i18n.Tag(),
		ImportConfig: setupImport,
		LinkTailscale: func(cli string) (string, error) {
			// cliLinks reads the file system only: safe off the main thread.
			return tailscaleTool(cli).Link(a.linkTailscaleDirs())
		},
	}, setup.Options{})
	v.flow.OnChange = func() { mygo.RunOnMain(a.invalidate) }
	return v.flow
}

// setupSteps are the steps to draw: the flow's, or a shot's.
func (a *app) setupSteps() []setup.StepState {
	if a.setupV.shotSteps != nil {
		return a.setupV.shotSteps
	}
	return a.setupFlow().Steps()
}

// setupDetect marks the steps already done, once per visit. Main thread.
func (a *app) setupDetect() {
	v := &a.setupV
	if a.shotMode || v.detected || v.running {
		return
	}
	v.detected = true
	f := a.setupFlow()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		f.Detect(ctx)
		mygo.RunOnMain(a.invalidate)
	}()
}

// runSetup runs the flow from the first step not done; with step, that
// step first (a retry). Main thread.
func (a *app) runSetup(step setup.Step) {
	v := &a.setupV
	if v.running || a.shotMode || !a.cfg.AllowWrites {
		return
	}
	f := a.setupFlow()
	f.Opts.SubscriptionURL = v.url
	f.Opts.ImportZip = v.zip
	v.running = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		var err error
		if step != "" {
			err = f.RunStep(ctx, step)
		}
		if err == nil {
			err = f.Run(ctx)
		}
		mygo.RunOnMain(func() {
			v.running = false
			if err == nil && f.Complete() {
				a.toast = i18n.T.SetupDone
				a.hc.Retry()
			}
			a.invalidate()
		})
	}()
}

func (a *app) setupPage(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	a.subPageHeader(c, T.SetupTitle)
	a.setupDetect()
	v := &a.setupV
	steps := a.setupSteps()
	state := map[setup.Step]setup.StepState{}
	complete := true
	for _, st := range steps {
		state[st.ID] = st
		if st.State != setup.Done && st.State != setup.Skipped {
			complete = false
		}
	}
	if sub := state[setup.StepSubscription]; sub.State != setup.Done && sub.State != setup.Skipped {
		a.setupSubscriptionCard(c, sub)
	}
	ui.Column(c).Gap(8).Children(func() {
		groupHead(c).Children(func() {
			ui.Text(c, T.SetupStepsHead).FontSize(plat.headingSize).FontWeight(plat.headingWeight)
			infoTipAbout(c, T.SetupStepsHead, T.TipSetup, nil)
		})
		listBox(c, c.Theme()).Children(func() {
			for i, st := range steps {
				a.setupStepRow(c.Key(fmt.Sprint("step-", st.ID)), i+1, st)
			}
		})
	})
	t := c.Theme()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		if complete {
			ui.Icon(c, iconOK).FontSize(iconLarge).TextColor(pal(t).okText)
			ui.Text(c, T.SetupDone).FontWeight(600).Grow(1)
			if pushButton(c.Key("overview"), T.SetupOpenOverv).Clicked() {
				a.showPage(pageOverview)
			}
			return
		}
		label := T.SetupStart
		if started(steps) {
			label = T.SetupContinue
		}
		if v.running {
			label = T.SetupRunning
		}
		needURL := state[setup.StepSubscription].State != setup.Done && v.url == "" && v.zip == nil
		b := busyButton(c.Key("run"), label, true, v.running, v.running || !s.WritesAllowed || needURL)
		disabledPrimaryFace(c, b)
		if b.Clicked() {
			a.runSetup("")
		}
		if !s.WritesAllowed {
			ui.Icon(c, iconSecure).FontSize(iconInline).TextColor(t.TextMuted).Tooltip(T.InstallNeedsWrites)
		}
	})
}

// started: some step ran or was found done (the button says 继续).
func started(steps []setup.StepState) bool {
	for _, st := range steps {
		if st.State != setup.Pending {
			return true
		}
	}
	return false
}

func (a *app) setupSubscriptionCard(c *ui.Context, sub setup.StepState) {
	T := i18n.T
	v := &a.setupV
	cardWith(c, T.SetupSubHead, func() {
		if setupImport == nil {
			return // HOOK: config.import not in this build
		}
		if pushButton(c.Key("import"), T.SetupImport).Disabled(v.running).Clicked() {
			a.pickSetupZip()
		}
	}, func() {
		ui.TextInput(c.Key("sub-url"), &v.url).Label(T.SetupSubURL).Placeholder(T.SetupSubPlaceholder).FillWidth().Disabled(v.running)
		if v.zipName != "" {
			small(c, i18n.F(T.SetupImported, v.zipName))
			warningNote(c, T.SetupImportSecret)
		}
	})
}

// pickSetupZip reads an export zip the person picks.
func (a *app) pickSetupZip() {
	parent := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: i18n.Cur().SetupImport,
			Filters: []mygo.FileFilter{{Name: "ZIP", Extensions: []string{"zip"}}}})
		if err != nil || len(paths) == 0 {
			return
		}
		b, err := readSetupZip(paths[0])
		mygo.RunOnMain(func() {
			if err != nil {
				a.toastErr = err.Error()
			} else {
				a.setupV.zip, a.setupV.zipName = b, baseName(paths[0])
			}
			a.invalidate()
		})
	}()
}

// setupReason words a failed step's reason from its code; "" when only
// the raw error is known (复制详情 has it).
func setupReason(st setup.StepState) string {
	T := i18n.T
	switch st.Code {
	case setup.CodeDownload:
		if r := i18n.PlainNetError(st.Error); r != "" {
			return T.SetupWhyDownload + T.LabelSep + r
		}
		return T.SetupWhyDownload
	case setup.CodeNotRoot:
		return T.SetupWhyNotRoot
	case setup.CodeInvalidConfig:
		return T.SetupWhyConfig
	case setup.CodeHealthFail:
		return i18n.F(T.SetupWhyHealth, st.Error)
	case setup.CodeNeedsSubscription:
		return T.SetupNeedsSub
	}
	return i18n.PlainNetError(st.Error)
}

func setupWarning(w setup.Warning) string {
	T := i18n.T
	switch w.Code {
	case setup.WarnGeo:
		return T.SetupWarnGeo
	case setup.WarnSystemVPN:
		return T.SetupWarnSystemVPN
	case setup.WarnTSOnPath:
		return i18n.F(T.SetupWarnTSOnPath, tildePath(w.Text))
	case setup.WarnTSLink:
		return T.SetupWarnTSLink
	}
	return w.Text
}

func (a *app) setupStepRow(c *ui.Context, n int, st setup.StepState) {
	t := c.Theme()
	T := i18n.T
	v := &a.setupV
	name := T.SetupSteps[string(st.ID)]
	listRow(c).Children(func() {
		ui.Column(c).Gap(6).FillWidth().Children(func() {
			if st.State == setup.Failed {
				// The failed row names the step itself: what, why, 重试.
				failedRowWhy(c.Key("failed"), name, setupReason(st), st.Error, func() { a.runSetup(st.ID) }, v.running)
				if st.Code == setup.CodeDownload {
					a.setupProxyChoices(c, st.ID)
				}
				return
			}
			ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Label(name).Children(func() {
				ui.Box(c).Width(16).AlignItems(ui.Center).Shrink(0).Children(func() {
					switch st.State {
					case setup.Running:
						spinnerIcon(c)
					case setup.Done:
						ui.Icon(c, iconOK).FontSize(iconInline).TextColor(pal(t).okText)
					case setup.Failed:
						ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(t.Warning)
					case setup.Waiting:
						if st.ID == setup.StepLogin {
							spinnerIcon(c)
						} else {
							ui.Icon(c, iconCollapsed).FontSize(iconInline).TextColor(t.Accent)
						}
					case setup.Skipped:
						ui.Icon(c, iconSkip).FontSize(iconInline).TextColor(t.TextMuted)
					default:
						num(ui.Text(c, fmt.Sprint(n)).FontSize(plat.smallSize).TextColor(t.TextMuted))
					}
				})
				label := ui.Text(c, name).SingleLine().Shrink(1)
				if st.State == setup.Pending || st.State == setup.Skipped {
					label.TextColor(t.TextMuted)
				}
				if st.ID == setup.StepDNS {
					infoTipAbout(c, name, T.TipSetupDNS, nil)
				}
				ui.Spacer(c)
				switch {
				case st.State == setup.Skipped:
					small(c, T.SetupSkipped)
				case st.Detail != "" && st.State == setup.Done:
					num(small(c, st.Detail)).SingleLine().Selectable()
				case st.State == setup.Waiting && st.URL != "":
					if pushButton(c.Key("login"), T.SetupOpenLogin).Clicked() {
						_ = mygo.Shell.OpenExternal(st.URL)
					}
				}
			})
			for i, w := range st.Warnings {
				small(c.Key(fmt.Sprint("warn", i)), setupWarning(w)).Wrap().LineHeight(lineText).TextColor(pal(t).warnText).PaddingStart(22)
			}
		})
	})
}

// setupProxyChoices are a failed download's other routes: the
// subscription's nodes (a temporary mihomo), or a proxy address.
func (a *app) setupProxyChoices(c *ui.Context, id setup.Step) {
	T := i18n.T
	v := &a.setupV
	ui.Column(c).Gap(8).PaddingStart(22).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			if pushButton(c.Key("via-sub"), T.SetupViaSub).Disabled(v.running).Clicked() {
				a.setupFlow().Opts.Proxy = rpc.ProxySubscription
				a.runSetup(id)
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
				a.runSetup(id)
			}
		})
	})
}

const maxSetupZip = 64 << 20

func readSetupZip(p string) ([]byte, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if fi.Size() > maxSetupZip {
		return nil, fmt.Errorf("%s: too large", filepath.Base(p))
	}
	return os.ReadFile(p)
}

func baseName(p string) string { return filepath.Base(p) }
