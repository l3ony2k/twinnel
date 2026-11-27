//go:build linux

package main

// Linux twin of takeover.go (darwin): same command, the systemd Probe from
// platform/linux.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/privfile"
	"github.com/l3ony2k/twinnel/core/takeover"
	"github.com/l3ony2k/twinnel/platform/linux"
)

func init() {
	registerOn(i18n.Linux, "takeover", "inspect a hand-made mihomo + Tailscale setup and write a takeover plan (read-only)", runTakeover)
}

const takeoverUsage = `usage: twinnel takeover plan [--base FILE] [--out DIR] [flags]

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
	fs := newFlags("takeover plan", takeoverUsage)
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
	plan, err := takeover.BuildPlan(ctx, linux.NewProbe(), opt)
	if err != nil {
		return err
	}
	if out == "" {
		if out, err = privfile.MkdirTemp("", "twinnel-takeover-"); err != nil {
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
