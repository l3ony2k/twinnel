package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// The first-run view, shown in place of the helper pages while no helper
// is installed: what the helper is for, what installing it does (the
// output of `twinnel helper install --dry-run`) and a button that runs the
// real install (one administrator prompt, through the CLI). The preview
// loads, then shows its steps, or a failed / timed-out state with 重试;
// the button never waits on it (the administrator prompt is the real
// confirmation). Only a development run's read-only switch locks it.

type onboardingView struct {
	loaded  bool
	loading bool
	plan    string
	planErr string
	timeout bool // the preview took longer than installPlanTimeout
	running bool
	out     string
	outErr  bool

	stepsOpen, fileOpen bool // the plan's disclosures
}

// resourcesDir is the app's resource directory (Contents/Resources in the
// bundle); a variable for tests.
var resourcesDir = func() (string, error) { return mygo.App.Path(mygo.PathResources) }

// cliPath finds the Twinnel command line tool: $TWINNEL_CLI, then the bundle's
// Contents/Resources/bin/twinnel (scripts/build-release.sh puts Twinnel and
// twinnel-helper there; Contents/MacOS/Twinnel is the app itself), then next to
// the app's executable, then $PATH.
func cliPath() (string, error) {
	if p := os.Getenv("TWINNEL_CLI"); p != "" {
		return p, nil
	}
	if dir, err := resourcesDir(); err == nil && dir != "" {
		p := filepath.Join(dir, "bin", "twinnel")
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, nil
		}
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "twinnel")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && p != exe {
			return p, nil
		}
	}
	if p, err := exec.LookPath("twinnel"); err == nil {
		return p, nil
	}
	return "", errors.New(i18n.T.CLINotFound)
}

// installArgs are the CLI arguments of `twinnel helper install`, naming the
// helper binary shipped next to the CLI (twinnel-helper) when there is one.
func installArgs(extra ...string) []string {
	args := []string{"helper", "install"}
	if cli, err := cliPath(); err == nil {
		if p := filepath.Join(filepath.Dir(cli), "twinnel-helper"); fileExists(p) {
			args = append(args, "--helper-bin", p)
		}
	}
	return append(args, extra...)
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// runCLI runs the CLI through the run's probes (fixtures in shot mode).
func (a *app) runCLI(ctx context.Context, args ...string) (string, error) {
	return a.probes.runCLI(ctx, args...)
}

// execCLI runs the CLI with args and returns its combined output.
func execCLI(ctx context.Context, args ...string) (string, error) {
	p, err := cliPath()
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, p, args...)
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	return out.String(), err
}

// installPlanTimeout bounds the dry run; a slower one shows as timed out.
var installPlanTimeout = 5 * time.Second

func (a *app) loadInstallPlan() {
	v := &a.hd.onb
	if v.loading {
		return
	}
	v.loading, v.timeout = true, false
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), installPlanTimeout)
		defer cancel()
		out, err := a.runCLI(ctx, installArgs("--dry-run")...)
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		mygo.RunOnMain(func() {
			v.loading, v.loaded = false, true
			v.plan, v.planErr, v.timeout = out, "", timedOut
			if err != nil {
				v.planErr = strings.TrimSpace(out)
				if v.planErr == "" {
					v.planErr = err.Error()
				}
			}
			a.invalidate()
		})
	}()
}

func (a *app) onboarding(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.onb
	if !v.loaded {
		a.loadInstallPlan()
	}
	groupBox(c, t).Gap(12).Padding(20, 20).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconTwinnel).FontSize(iconHero).TextColor(t.Accent)
			ui.Text(c, T.OnboardTitle).FontSize(plat.titleSize - 3).FontWeight(plat.titleWeight)
		})
		ui.Text(c, T.OnboardIntro).Wrap().LineHeight(lineText)
		ui.Column(c).Gap(6).Children(func() {
			for _, line := range T.OnboardPoints {
				ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
					// Bullets, not actions: muted, so the one accent on the
					// card is 安装后台助手 (R-unslop-10).
					ui.Icon(c, iconOK).FontSize(iconInline).TextColor(t.TextMuted).MarginY(1).Shrink(0)
					ui.Text(c, line).Grow(1).Wrap().LineHeight(lineText)
				})
			}
		})
		hintText(c, T.OnboardReadOnly)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(4, 0, 0, 0).Children(func() {
			canInstall := s.WritesAllowed && !v.running
			label := T.OnboardInstall
			if v.running {
				label = T.OnboardInstalling
			}
			if iconButton(c.Key("install"), iconOK, label, true).Disabled(!canInstall).Clicked() {
				a.installHelper()
			}
			if v.running {
				ui.Spinner(c)
			}
			if !s.WritesAllowed {
				// A development run's read-only switch: a lock, its
				// reason in the tooltip.
				ui.Icon(c, iconSecure).FontSize(iconInline).TextColor(t.TextMuted).Tooltip(T.InstallNeedsWrites)
			}
			ui.Spacer(c)
			if pushButton(c.Key("recheck"), T.HelperRetry).Clicked() {
				v.loaded = false
				a.hc.Retry()
			}
		})
		if v.out != "" {
			if v.outErr {
				// The CLI's output is the plan plus one "twinnel: …" line
				// saying what went wrong: show that line, and the rest
				// only as the copyable detail (never parse the plan).
				errorNote(c, cliFailure(v.out, nil))
			} else {
				codeBlock(c, v.out)
			}
		}
	})
	a.installPlanCard(c)
}

// installPlanCard is what installing does: the dry-run's steps in words,
// one per row, the command under each, behind a disclosure.
func (a *app) installPlanCard(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.onb
	steps, parsed := parsePlan(v.plan)
	ui.Column(c).Gap(8).Children(func() {
		groupHead(c).Children(func() {
			ui.Text(c, T.OnboardPlan).FontSize(plat.headingSize).FontWeight(plat.headingWeight)
			infoTipAbout(c, T.OnboardPlan, T.TipInstallPlan, nil)
		})
		hintText(c, T.OnboardPlanHint)
		listBox(c, t).Children(func() {
			switch {
			case v.loading && v.plan == "":
				listRow(c).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c).Label(T.Loading) // the spinner says it (R-unslop-29)
					})
				})
			case v.planErr != "" || v.timeout:
				// What failed, why in words when known, 重试 and 复制详情
				// (kubai R-10).
				listRow(c).Children(func() {
					head := T.InstallPlanFailed
					if v.timeout {
						head = T.InstallPlanTimeout
					}
					failedRow(c.Key("plan-failed"), head, v.planErr, a.loadInstallPlan, v.loading)
				})
			case !parsed && v.plan != "":
				listRow(c).Children(func() { codeBlock(c, v.plan) })
			case parsed:
				head := ui.Row(c.Key("steps")).Gap(8).AlignItems(ui.Center).PaddingY(listRowPadY).
					Transition(colorMotion).Focusable().Role(ui.RoleDisclosure).Expanded(v.stepsOpen).Label(i18n.F(T.OnboardSteps, len(steps)))
				rowFill(c, rowBleed(head), false)
				head.Children(func() {
					disclosure(c, v.stepsOpen).Shrink(0)
					ui.Text(c, i18n.F(T.OnboardSteps, len(steps))).FontWeight(500)
				})
				if head.Clicked() {
					v.stepsOpen = !v.stepsOpen
				}
				if !v.stepsOpen {
					return
				}
				for i, st := range steps {
					a.installStepRow(c.Key(i18n.F("step%d", i)), i+1, st)
				}
			}
		})
	})
}

func (a *app) installStepRow(c *ui.Context, n int, st planStep) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.onb
	ui.Row(c).Gap(10).AlignItems(ui.Start).PaddingY(8).PaddingStart(22).Clip().Transition(expandFor(true)).Children(func() {
		num(ui.Text(c, i18n.F("%d", n)).FontSize(plat.smallSize).TextColor(t.TextMuted).Width(14).TextAlign(ui.End).MarginY(2).Shrink(0))
		ui.Column(c).Gap(3).Grow(1).Shrink(1).Children(func() {
			ui.Text(c, st.what).Wrap().LineHeight(lineText)
			if st.what != st.cmd {
				ui.Text(c, st.cmd).Font("monospace").FontSize(plat.smallSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText).Selectable()
			}
			if st.content == "" {
				return
			}
			label := T.OnboardShowFile
			if v.fileOpen {
				label = T.OnboardHideFile
			}
			if linkDisclosure(c.Key("file"), v.fileOpen, label).Clicked() {
				v.fileOpen = !v.fileOpen
			}
			if v.fileOpen {
				codeBlock(c, st.content)
			}
		})
	})
}

// codeBlock shows text a person may copy: a file, a command's output.
func codeBlock(c *ui.Context, s string) ui.Element {
	t := c.Theme()
	return ui.Column(c).Radius(plat.controlRadius).Background(pal(t).code).Border(1, pal(t).groupLine).Children(func() {
		ui.Text(c, strings.TrimRight(s, "\n")).Font("monospace").FontSize(plat.smallSize).Padding(10, 12).Wrap().LineHeight(lineText).Selectable()
	})
}

// installHelper runs `twinnel helper install`, which asks for the
// administrator password (osascript) and sets up the LaunchDaemon. Does
// nothing with TWINNEL_READ_ONLY=1.
func (a *app) installHelper() {
	v := &a.hd.onb
	if !a.cfg.AllowWrites || v.running {
		return
	}
	v.running, v.out, v.outErr = true, "", false
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		// The click is the confirmation (macOS then asks for the
		// administrator password), so the CLI must not prompt on stdin.
		out, err := a.runCLI(ctx, installArgs("--yes")...)
		mygo.RunOnMain(func() {
			v.running = false
			v.out, v.outErr = out, err != nil
			if err != nil && out == "" {
				v.out = err.Error()
			}
			a.hc.Retry()
			a.invalidate()
		})
	}()
}
