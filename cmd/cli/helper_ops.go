package main

// Helper RPC commands added for service control, geo databases, the
// external UI and health.

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/rpc/healthsrc"
)

var serviceMethods = map[string]string{
	"start": rpc.MethodServiceStart, "stop": rpc.MethodServiceStop, "restart": rpc.MethodServiceRestart,
	"install": rpc.MethodServiceInstall, "uninstall": rpc.MethodServiceUninstall,
}

func serviceOp(cf *clientFlags, op, name string) error {
	method, ok := serviceMethods[op]
	if !ok {
		return fmt.Errorf("service: unknown operation %q (status, start, stop, restart, install, uninstall)", op)
	}
	var r rpc.ServiceOpResult
	return cf.callPrint(method, rpc.ServiceParams{Name: name}, &r, func() {
		if r.DryRun {
			fmt.Fprintf(stdout, "# dry run: the helper is not running as root, nothing was run\n")
		}
		fmt.Fprintf(stdout, "%s %s (%s):\n", r.Op, r.Name, r.Label)
		for i, s := range r.Steps {
			fmt.Fprintf(stdout, "  %d. %s\n", i+1, s)
		}
		fmt.Fprintf(stdout, "now: installed=%v loaded=%v running=%v", r.Status.Installed, r.Status.Loaded, r.Status.Running)
		if r.Status.PID > 0 {
			fmt.Fprintf(stdout, " pid=%d", r.Status.PID)
		}
		fmt.Fprintln(stdout)
	})
}

func runAccessory(kind string, args []string) error {
	fs, cf := newFlagSet("kernel "+kind, fmt.Sprintf("usage: duet kernel %s check|update [--force]\n\n", kind))
	var force bool
	fs.BoolVar(&force, "force", false, "download even what looks up to date")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if len(pos) != 1 || (pos[0] != "check" && pos[0] != "update") {
		fs.Usage()
		return errors.New("want check or update")
	}
	check, update := rpc.MethodGeoCheck, rpc.MethodGeoUpdate
	if kind == "ui" {
		check, update = rpc.MethodUICheck, rpc.MethodUIUpdate
	}
	if pos[0] == "check" {
		var c rpc.AccessoryCheck
		return cf.callPrint(check, nil, &c, func() {
			fmt.Fprintf(stdout, "dir: %s\n", c.Dir)
			printAccessories(stdout, c.Files)
		})
	}
	var r rpc.AccessoryUpdateResult
	return cf.callPrint(update, rpc.AccessoryUpdateParams{Force: force}, &r, func() {
		fmt.Fprintf(stdout, "updated: %s\n", orNone(strings.Join(r.Updated, ", ")))
		if len(r.Skipped) > 0 {
			fmt.Fprintf(stdout, "up to date: %s\n", strings.Join(r.Skipped, ", "))
		}
		printAccessories(stdout, r.Files)
		if r.ReloadNeeded {
			fmt.Fprintln(stdout, "mihomo reads the new files at its next reload: duet compose --apply --force (or restart the service)")
		}
	})
}

func printAccessories(w io.Writer, files []rpc.AccessoryFile) {
	t := tw()
	fmt.Fprintln(t, "NAME\tINSTALLED\tLOCAL\tREMOTE\tVERIFIED\tUPDATE\tFILE")
	for _, f := range files {
		local := "-"
		if f.Installed {
			local = short12(f.SHA256)
		}
		remote := short12(f.Remote)
		if f.Error != "" {
			remote = "error"
		}
		fmt.Fprintf(t, "%s\t%v\t%s\t%s\t%v\t%v\t%s\n", f.Name, f.Installed, local, orDash(remote), f.Verifiable, f.UpdateAvailable, f.File)
	}
	t.Flush()
	for _, f := range files {
		if f.Error != "" {
			fmt.Fprintf(w, "%s: %s\n", f.Name, f.Error)
		}
	}
}

func short12(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func printHealth(w io.Writer, r rpc.HealthReport) {
	rep, err := healthsrc.Decode(r)
	if err != nil {
		fmt.Fprintln(w, err)
		return
	}
	if rep == nil {
		fmt.Fprintln(w, "no health report yet (duet helper health --run)")
		return
	}
	fmt.Fprintf(w, "overall %s, %s (%s), reason %q\n", rep.Overall, r.At.Local().Format("15:04:05"), rep.Duration.Round(1e6), r.Reason)
	t := tw()
	for _, c := range rep.Checks {
		fmt.Fprintf(t, "%s\t%s\t%s\n", c.Status, c.ID, c.Summary)
	}
	t.Flush()
}
