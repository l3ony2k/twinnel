package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	card(c, "", func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconDuet).FontSize(22).TextColor(t.Accent)
			ui.Text(c, T.OnboardTitle).FontSize(15).FontWeight(600)
		})
		ui.Text(c, T.OnboardIntro).FontSize(13).Wrap()
		for _, line := range T.OnboardPoints {
			ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
				dot(c, t.TextMuted, 5).MarginY(6)
				ui.Text(c, line).FontSize(13).Grow(1).Wrap()
			})
		}
		ui.Text(c, T.OnboardReadOnly).FontSize(12).TextColor(t.TextMuted).Wrap()
	})
	card(c, T.OnboardPlan, func() {
		ui.Text(c, T.OnboardPlanHint).FontSize(12).TextColor(t.TextMuted).Wrap()
		switch {
		case v.loading && v.plan == "":
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, T.Loading).FontSize(12).TextColor(t.TextMuted)
			})
		case v.plan != "":
			ui.Scroll(c).Height(260).Radius(plat.controlRadius+2).Background(pal(t).code).Border(1, pal(t).groupLine).Children(func() {
				ui.Text(c, v.plan).Font("monospace").FontSize(11).Padding(10, 12).Wrap().Selectable()
			})
		}
		if v.planErr != "" {
			ui.Text(c, v.planErr).FontSize(12).TextColor(pal(t).badText).Wrap().Selectable()
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			canInstall := s.WritesAllowed && !v.running && v.planErr == "" && v.plan != ""
			label := T.OnboardInstall
			if v.running {
				label = T.OnboardInstalling
			}
			if iconButton(c.Key("install"), iconOK, label, true).Disabled(!canInstall).Clicked() {
				a.installHelper()
			}
			if ui.Button(c.Key("recheck"), T.HelperRetry).Clicked() {
				v.loaded = false
				a.hc.Retry()
			}
			if !s.WritesAllowed {
				ui.Text(c, T.WritesDisabledHint).FontSize(12).TextColor(t.TextMuted).Shrink(1).Wrap()
			}
		})
		if v.out != "" {
			col := t.Text
			if v.outErr {
				col = t.Danger
			}
			ui.Text(c, v.out).Font("monospace").FontSize(11).TextColor(col).Wrap().Selectable()
		}
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
