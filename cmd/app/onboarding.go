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

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
)

// The first-run view, shown in place of the helper pages while no helper
// is installed: what the helper is for, what installing it does (the
// output of `duet helper install --dry-run`) and a button that runs the
// real install (one administrator prompt, through the CLI). The button is
// disabled unless DUET_ALLOW_WRITES=1.

type onboardingView struct {
	loaded  bool
	loading bool
	plan    string
	planErr string
	running bool
	out     string
	outErr  bool

	stepsOpen, fileOpen bool // the plan's disclosures
}

// resourcesDir is the app's resource directory (Contents/Resources in the
// bundle); a variable for tests.
var resourcesDir = func() (string, error) { return mygo.App.Path(mygo.PathResources) }

// cliPath finds the duet command line tool: $DUET_CLI, then the bundle's
// Contents/Resources/bin/duet (scripts/build-release.sh puts duet and
// duet-helper there; Contents/MacOS/duet is the app itself), then next to
// the app's executable, then $PATH.
func cliPath() (string, error) {
	if p := os.Getenv("DUET_CLI"); p != "" {
		return p, nil
	}
	if dir, err := resourcesDir(); err == nil && dir != "" {
		p := filepath.Join(dir, "bin", "duet")
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, nil
		}
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "duet")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && p != exe {
			return p, nil
		}
	}
	if p, err := exec.LookPath("duet"); err == nil {
		return p, nil
	}
	return "", errors.New(i18n.T.CLINotFound)
}

// installArgs are the CLI arguments of `duet helper install`, naming the
// helper binary shipped next to the CLI (duet-helper) when there is one.
func installArgs(extra ...string) []string {
	args := []string{"helper", "install"}
	if cli, err := cliPath(); err == nil {
		if p := filepath.Join(filepath.Dir(cli), "duet-helper"); fileExists(p) {
			args = append(args, "--helper-bin", p)
		}
	}
	return append(args, extra...)
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// runCLI runs the CLI with args and returns its combined output.
func runCLI(ctx context.Context, args ...string) (string, error) {
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

func (a *app) loadInstallPlan() {
	v := &a.hd.onb
	if v.loading {
		return
	}
	v.loading = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := runCLI(ctx, installArgs("--dry-run")...)
		mygo.RunOnMain(func() {
			v.loading, v.loaded = false, true
			v.plan, v.planErr = out, ""
			if err != nil {
				v.planErr = i18n.T.ErrorPrefix + err.Error()
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
			ui.Icon(c, iconDuet).FontSize(28).TextColor(t.Accent)
			ui.Text(c, T.OnboardTitle).FontSize(plat.titleSize - 3).FontWeight(plat.titleWeight)
		})
		ui.Text(c, T.OnboardIntro).Wrap()
		ui.Column(c).Gap(6).Children(func() {
			for _, line := range T.OnboardPoints {
				ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
					ui.Icon(c, iconOK).FontSize(14).TextColor(t.Accent).MarginY(1).Shrink(0)
					ui.Text(c, line).Grow(1).Wrap()
				})
			}
		})
		hintText(c, T.OnboardReadOnly)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(4, 0, 0, 0).Children(func() {
			canInstall := s.WritesAllowed && !v.running && v.planErr == "" && v.plan != ""
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
			if ui.Button(c.Key("recheck"), T.HelperRetry).Clicked() {
				v.loaded = false
				a.hc.Retry()
			}
		})
		if !s.WritesAllowed {
			ui.Row(c).Gap(6).AlignItems(ui.Start).Children(func() {
				ui.Icon(c, iconSecure).FontSize(13).TextColor(t.TextMuted).MarginY(1).Shrink(0)
				hintText(c, T.WritesDisabledHint).Grow(1)
			})
		}
		if v.out != "" {
			if v.outErr {
				errorNote(c, strings.TrimSpace(v.out))
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
		ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(4).MinHeight(28).Children(func() {
			ui.Text(c, T.OnboardPlan).FontSize(plat.headingSize).FontWeight(plat.headingWeight)
		})
		hintText(c, T.OnboardPlanHint).PaddingX(4)
		listBox(c, t).Children(func() {
			switch {
			case v.loading && v.plan == "":
				listRow(c).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c)
						small(c, T.Loading)
					})
				})
			case v.planErr != "":
				listRow(c).Children(func() { errorNote(c, v.planErr) })
			case !parsed && v.plan != "":
				listRow(c).Children(func() { codeBlock(c, v.plan) })
			case parsed:
				head := ui.Row(c.Key("steps")).Gap(8).AlignItems(ui.Center).PaddingY(10).Cursor(ui.CursorPointer).
					Focusable().Role(ui.RoleDisclosure).Expanded(v.stepsOpen).Label(i18n.F(T.OnboardSteps, len(steps)))
				head.Children(func() {
					disclosure(c, v.stepsOpen)
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
			ui.Text(c, st.what).Wrap()
			if st.what != st.cmd {
				ui.Text(c, st.cmd).Font("monospace").FontSize(plat.smallSize).TextColor(t.TextMuted).Wrap().Selectable()
			}
			if st.content == "" {
				return
			}
			label := T.OnboardShowFile
			if v.fileOpen {
				label = T.OnboardHideFile
			}
			disc := ui.Row(c.Key("file")).Gap(4).AlignItems(ui.Center).Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleDisclosure).Expanded(v.fileOpen).Label(label)
			disc.Children(func() {
				ui.Icon(c, iconCollapsed).FontSize(11).TextColor(t.Accent).Transition(turnMotion).Rotate(map[bool]float32{true: 90}[v.fileOpen])
				ui.Text(c, label).FontSize(plat.smallSize).TextColor(t.Accent)
			})
			if disc.Clicked() {
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
		ui.Text(c, strings.TrimRight(s, "\n")).Font("monospace").FontSize(plat.smallSize).Padding(10, 12).Wrap().Selectable()
	})
}

// installHelper runs `duet helper install`, which asks for the
// administrator password (osascript) and sets up the LaunchDaemon. Only
// reachable with DUET_ALLOW_WRITES=1.
func (a *app) installHelper() {
	v := &a.hd.onb
	if !a.cfg.AllowWrites || v.running {
		return
	}
	v.running, v.out, v.outErr = true, "", false
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		out, err := runCLI(ctx, installArgs()...)
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
