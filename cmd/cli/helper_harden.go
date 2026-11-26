package main

// duet helper harden plan|apply: move an adopted mihomo to the managed
// kernel, a root-owned config dir and duet's LaunchDaemon (one restart).

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/l3ony2k/duet/core/rpc"
)

const hardenUsage = `usage: duet helper harden plan [--kernel VERSION] [--new-secret] [--json]
       duet helper harden apply [PLAN_ID] [--yes] [--skip-health] [--json]

Moves the adopted mihomo service (a root daemon running a binary and a
config dir that your user can change) to duet's managed kernel, a
root-owned config dir under the helper's state dir and duet's own
LaunchDaemon. This RESTARTS mihomo once: the network is down for a few
seconds. The old plist, binary and config dir stay on disk (the old label
is only disabled), so it can be undone by hand (runbook step 8).

plan    shows what would happen; changes nothing.
apply   shows the plan (the newest ready one, or PLAN_ID), asks, and runs it.

`

func runHelperHarden(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "plan":
		return runHardenPlan(args)
	case "apply":
		return runHardenApply(args)
	}
	fmt.Fprint(stdout, hardenUsage)
	return errors.New("harden: want plan or apply")
}

func runHardenPlan(args []string) error {
	fs, cf := newFlagSet("helper harden plan", hardenUsage)
	var p rpc.HardenPlanParams
	fs.StringVar(&p.KernelVersion, "kernel", "", "managed kernel `VERSION` (default: the version that runs now)")
	fs.BoolVar(&p.NewSecret, "new-secret", false, "start the new mihomo with a new controller secret")
	if _, done, err := parse(fs, args); done || err != nil {
		return err
	}
	var plan rpc.HardenPlan
	return cf.callPrint(rpc.MethodHardenPlan, p, &plan, func() {
		printHardenPlan(stdout, plan)
		fmt.Fprintf(stdout, "\nrun it (when nothing is downloading): duet helper harden apply %s\n", plan.PlanID)
	})
}

func runHardenApply(args []string) error {
	fs, cf := newFlagSet("helper harden apply", hardenUsage)
	var yes, skipHealth bool
	fs.BoolVar(&yes, "yes", false, "do not ask for confirmation")
	fs.BoolVar(&skipHealth, "skip-health", false, "verify only /version, not the health checks (no rollback on worse health)")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	id := ""
	if len(pos) > 0 {
		id = pos[0]
	}
	var plans []rpc.HardenPlan
	if err := cf.call(rpc.MethodHardenPlans, rpc.HardenPlansParams{PlanID: id}, &plans); err != nil {
		return err
	}
	var plan *rpc.HardenPlan
	for i := range plans {
		if plans[i].State == rpc.PlanReady {
			plan = &plans[i]
			break
		}
	}
	if plan == nil {
		if id != "" {
			return fmt.Errorf("plan %s is not ready (%s); make a new one with `duet helper harden plan`", id, plans[0].State)
		}
		return errors.New("no ready harden plan; make one with `duet helper harden plan`")
	}
	if !cf.json || !yes {
		printHardenPlan(stdout, *plan)
		fmt.Fprintf(stdout, "\nmihomo restarts now: about %d–%d s without network.\n", plan.Downtime.MinSeconds, plan.Downtime.MaxSeconds)
		if skipHealth {
			fmt.Fprintln(stdout, "--skip-health: only /version is checked; worse health does not roll back")
		}
	}
	if !yes && !confirm() {
		return errors.New("harden cancelled")
	}
	var r rpc.HardenResult
	cf.timeout = max(cf.timeout, 6*time.Minute)
	err = cf.callPrint(rpc.MethodHardenApply, rpc.HardenApplyParams{PlanID: plan.PlanID, ConfirmHash: plan.ConfirmHash, SkipHealth: skipHealth}, &r, func() {
		printHardenResult(stdout, r)
	})
	if err != nil {
		return err
	}
	if !r.OK {
		return fmt.Errorf("harden did not complete: %s", r.Error)
	}
	return nil
}

func printHardenPlan(w io.Writer, p rpc.HardenPlan) {
	fmt.Fprintf(w, "harden plan %s (%s)\n", p.PlanID, p.State)
	side := func(name string, s rpc.HardenService) {
		fmt.Fprintf(w, "%s  %s (%s)\n      binary %s %s\n      config %s (dir owned by %s)\n", name, s.Label, s.Plist, s.Binary, s.Version, s.ConfigFile, s.Owner)
	}
	side("from", p.From)
	side("to  ", p.To)
	k := p.Kernel
	if k.Installed {
		fmt.Fprintf(w, "kernel  %s (installed)\n", k.Version)
	} else {
		fmt.Fprintf(w, "kernel  %s (download %s, %.1f MB)\n", k.Version, k.Asset, float64(k.Size)/1e6)
	}
	fmt.Fprintf(w, "copy    %d items, %.1f MB:", len(p.Copy), float64(p.CopyBytes)/1e6)
	for _, f := range p.Copy {
		if f.Dir {
			fmt.Fprintf(w, " %s/", f.Path)
		} else {
			fmt.Fprintf(w, " %s", f.Path)
		}
	}
	fmt.Fprintln(w)
	for _, s := range p.Skipped {
		fmt.Fprintf(w, "        not copied: %s\n", s)
	}
	fmt.Fprintf(w, "secret  %s\n", p.Secret)
	for _, c := range p.ConfigChanges {
		fmt.Fprintf(w, "change  %s\n", c)
	}
	fmt.Fprintf(w, "offline about %d–%d s (%s)\n", p.Downtime.MinSeconds, p.Downtime.MaxSeconds, p.Downtime.Note)
	fmt.Fprintln(w, "steps:")
	phase := ""
	for _, s := range p.Steps {
		if s.Phase != phase {
			phase = s.Phase
			if phase == "rollback" {
				fmt.Fprintln(w, "  rollback (only if the new mihomo fails):")
			} else {
				fmt.Fprintf(w, "  %s:\n", phase)
			}
		}
		mark := " "
		if s.Network {
			mark = "!"
		}
		fmt.Fprintf(w, "   %s %s\n", mark, s.Text)
	}
	fmt.Fprintln(w, "  (! = mihomo is not running during this step)")
	for _, x := range p.Warnings {
		fmt.Fprintf(w, "warning: %s\n", x)
	}
	if p.DryRun {
		fmt.Fprintln(w, "DRY RUN: the helper is not root; apply prepares and validates only")
	}
}

func printHardenResult(w io.Writer, r rpc.HardenResult) {
	fmt.Fprintf(w, "\nharden %s\n", r.PlanID)
	for i, s := range r.Steps {
		fmt.Fprintf(w, "  %2d. %-15s %-7s %s\n", i+1, s.Name, s.Status, s.Detail)
	}
	for _, c := range r.HealthAfter {
		fmt.Fprintf(w, "  health %-5s %-25s %s\n", c.Status, c.ID, truncate(c.Summary, 100))
	}
	for _, x := range r.Warnings {
		fmt.Fprintf(w, "warning: %s\n", x)
	}
	if r.DowntimeMS > 0 {
		fmt.Fprintf(w, "offline:     %.1f s\n", float64(r.DowntimeMS)/1000)
	}
	switch {
	case r.OK && r.DryRun:
		fmt.Fprintln(w, "result:      dry run OK (nothing was stopped or started)")
	case r.OK:
		fmt.Fprintln(w, "result:      mihomo now runs from duet's kernel and config dir")
	case r.RolledBack:
		fmt.Fprintf(w, "result:      ROLLED BACK: %s\n", r.Error)
	default:
		fmt.Fprintf(w, "result:      FAILED: %s\n", r.Error)
	}
}
