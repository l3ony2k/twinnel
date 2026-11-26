//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/tailscale"
	"github.com/l3ony2k/duet/core/takeover"
	"github.com/l3ony2k/duet/platform/darwin"
)

func init() {
	register("takeover", "inspect a hand-made mihomo + Tailscale setup and write a takeover plan (read-only)", runTakeover)
}

const takeoverUsage = `usage: duet takeover plan [--base FILE] [--out DIR] [flags]

Finds the running mihomo service and Tailscale, picks the subscription the
live config was built from, derives override layers (base settings, user
rules, managed Tailscale coexist) and checks that composing them reproduces
the live config (mihomo -t + semantic diff). Writes the plan, the layer files
and the composed config into DIR (default: a new temp dir) and prints a
summary. Nothing on the system is changed; there is no apply command.

flags:
`

func runTakeover(args []string) error {
	if len(args) == 0 || args[0] != "plan" {
		fmt.Fprint(os.Stderr, takeoverUsage)
		return errors.New("takeover: only the \"plan\" subcommand exists")
	}
	fs := flag.NewFlagSet("takeover plan", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), takeoverUsage)
		fs.PrintDefaults()
	}
	var opt takeover.Options
	var out string
	var asJSON bool
	fs.StringVar(&opt.BasePath, "base", "", "subscription base `FILE` (default: best candidate next to the live config)")
	fs.StringVar(&out, "out", "", "output `DIR` (default: a new temp dir)")
	fs.StringVar(&opt.ConfigPath, "config", "", "live mihomo config `FILE` (default: from the service's -d/-f)")
	fs.StringVar(&opt.MihomoBin, "mihomo", "", "mihomo `BIN` for validation (default: the service's binary)")
	fs.BoolVar(&opt.NoValidate, "no-validate", false, "skip mihomo -t")
	fs.BoolVar(&asJSON, "json", false, "print plan.json instead of the summary")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("takeover plan: unexpected arguments %q", fs.Args())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	plan, err := takeover.BuildPlan(ctx, newDarwinProbe(), opt)
	if err != nil {
		return err
	}
	if out == "" {
		if out, err = os.MkdirTemp("", "duet-takeover-"); err != nil {
			return err
		}
	} else if entries, err := os.ReadDir(out); err == nil && len(entries) > 0 {
		if _, err := os.Stat(filepath.Join(out, "plan.json")); err != nil {
			return fmt.Errorf("takeover plan: %s is not empty and holds no earlier plan; choose another --out", out)
		}
	}
	if err := plan.WriteFiles(out); err != nil {
		return err
	}
	if asJSON {
		b, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(append(b, '\n'))
		return err
	}
	fmt.Print(plan.Text())
	return nil
}

// darwinProbe is the read-only takeover.Probe for macOS.
type darwinProbe struct {
	run  darwin.CommandRunner
	home string
}

func newDarwinProbe() *darwinProbe {
	home, _ := os.UserHomeDir()
	return &darwinProbe{run: darwin.ExecRunner(10 * time.Second), home: home}
}

func (p *darwinProbe) UserHome() string { return p.home }

// Services reads launchd plists (daemons and the user's agents) that mention
// mihomo, clash or tailscale.
func (p *darwinProbe) Services(ctx context.Context) ([]takeover.ServiceDef, error) {
	var out []takeover.ServiceDef
	dirs := []struct{ dir, kind string }{
		{"/Library/LaunchDaemons", "launchd-daemon"},
		{"/Library/LaunchAgents", "launchd-agent"},
		{filepath.Join(p.home, "Library", "LaunchAgents"), "launchd-agent"},
	}
	for _, d := range dirs {
		plists, _ := filepath.Glob(filepath.Join(d.dir, "*.plist"))
		for _, pl := range plists {
			b, err := os.ReadFile(pl)
			if err != nil {
				continue
			}
			s := strings.ToLower(string(b))
			if !strings.Contains(s, "mihomo") && !strings.Contains(s, "clash") && !strings.Contains(s, "tailscale") {
				continue
			}
			js, err := p.run(ctx, "/usr/bin/plutil", "-convert", "json", "-o", "-", pl)
			if err != nil {
				continue
			}
			var v struct {
				Label            string
				Program          string
				ProgramArguments []string
				UserName         string
			}
			if json.Unmarshal(js, &v) != nil {
				continue
			}
			prog := v.ProgramArguments
			if len(prog) == 0 && v.Program != "" {
				prog = []string{v.Program}
			}
			out = append(out, takeover.ServiceDef{Label: v.Label, Path: pl, Kind: d.kind, Program: prog, User: v.UserName})
		}
	}
	return out, nil
}

func (p *darwinProbe) MihomoVersion(ctx context.Context, controller, secret string) (string, error) {
	c, err := mihomo.New(mihomo.Options{Address: controller, Secret: secret, Timeout: 3 * time.Second})
	if err != nil {
		return "", err
	}
	v, err := c.Version(ctx)
	if err != nil {
		return "", err
	}
	return v.Version, nil
}

func (p *darwinProbe) TailscaleStatus(ctx context.Context) ([]byte, error) { return tailscaleStatus() }

func (p *darwinProbe) TailscaleInterface(ctx context.Context) (string, error) {
	return tailscaleIface()
}

func (p *darwinProbe) TailscaleInstall() tailscale.Install {
	return tailscale.DetectInstall(runtime.GOOS, func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	})
}
