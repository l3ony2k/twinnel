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
	"time"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/privfile"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/takeover"
	"github.com/l3ony2k/twinnel/platform/darwin/takeoverprobe"
)

func init() {
	registerOn(i18n.Darwin, "takeover", "inspect a hand-made mihomo + Tailscale setup and write a takeover plan (read-only)", runTakeover)
}

const takeoverUsage = `usage: twinnel takeover plan [--base FILE] [--out DIR] [flags]

Finds the running mihomo service and Tailscale, picks the subscription the
live config was built from, derives override layers (base settings, user
rules, managed Tailscale coexist) and checks that composing them reproduces
the live config (mihomo -t + semantic diff). Writes the plan, the layer files
and the composed config into DIR (default: a new temp dir) and prints a
summary. Nothing on the system is changed.

With --via-helper the helper makes the plan itself and keeps it under its
state dir; adopt it with ` + "`twinnel helper adopt PLAN_ID`" + `.

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
	var asJSON, viaHelper bool
	var socket string
	fs.StringVar(&opt.BasePath, "base", "", "subscription base `FILE` (default: best candidate next to the live config)")
	fs.StringVar(&out, "out", "", "output `DIR` (default: a new temp dir)")
	fs.StringVar(&opt.ConfigPath, "config", "", "live mihomo config `FILE` (default: from the service's -d/-f)")
	fs.StringVar(&opt.MihomoBin, "mihomo", "", "mihomo `BIN` for validation (default: the service's binary)")
	fs.BoolVar(&opt.NoValidate, "no-validate", false, "skip mihomo -t")
	fs.BoolVar(&asJSON, "json", false, "print plan.json instead of the summary")
	fs.BoolVar(&viaHelper, "via-helper", false, "let the helper make the plan and keep it under its state dir (for 'twinnel helper adopt')")
	fs.StringVar(&socket, "socket", rpc.SocketFromEnv(), "helper socket `PATH` (with --via-helper)")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("takeover plan: unexpected arguments %q", fs.Args())
	}
	if viaHelper {
		if out != "" || opt.ConfigPath != "" || opt.MihomoBin != "" || opt.NoValidate {
			return errors.New("takeover plan --via-helper: only --base and --json apply (the helper discovers the setup itself)")
		}
		return takeoverPlanViaHelper(socket, opt.BasePath, asJSON)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	plan, err := takeover.BuildPlan(ctx, newDarwinProbe(), opt)
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

func newDarwinProbe() *takeoverprobe.Probe {
	home, _ := os.UserHomeDir()
	return takeoverprobe.New(home)
}
