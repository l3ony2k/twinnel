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
	"github.com/l3ony2k/twinnel/core/msg"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/setup"
)

// core/setup's Flow as the app drives it: the first-run wizard
// (wizard.go) runs its steps; this file builds the flow on the helper
// connection and the CLI, detects what is already done, and draws a step's
// row with its state (④'s progress list).

type setupView struct {
	flow     *setup.Flow
	detected bool
	running  bool
	url      string
	// proxyOpen shows the proxy address field of a failed download.
	proxyOpen bool
	proxyAddr string
	// shotSteps replaces the flow's steps in shot mode (shots_setup.go).
	shotSteps []setup.StepState
}

// setupImport restores a picked file through the helper's config.import
// (setup.ImportConfig); nil hides 导入配置文件….
var setupImport = setup.ImportConfig

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
		// Windows: the login runs in this, the signed-in user's, process.
		SessionLogin: setup.LocalSessionLogin,
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
	case setup.CodeInstallFailed:
		// The helper's coded message (the installer's exit, a binary
		// that does not run), worded when Twinnel knows it.
		if m, ok := T.Msg(msg.Parse(st.Error)); ok {
			return T.SetupWhyInstall + T.LabelSep + m
		}
		return T.SetupWhyInstall
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
				failedRowWhy(c.Key("failed"), name, setupReason(st), st.Error, func() { a.wizRetry(st.ID) }, v.running)
				if st.Code == setup.CodeDownload {
					a.wizDownloadChoices(c, true)
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
				case st.State == setup.Running && st.ID == setup.StepApply:
					// What the apply waits for (save-perf's compose.stage).
					small(c, stageText(a.hd.applyStage)).SingleLine()
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
