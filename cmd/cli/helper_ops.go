package main

// Helper RPC commands added for service control, geo databases, the
// external UI and health.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/rpc/healthsrc"
	"github.com/l3ony2k/twinnel/core/service"
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
			fmt.Fprintf(stdout, "# dry run: the helper is not running %s, nothing was run\n", service.Current().AsAdmin())
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

// logFlags are `twinnel service log`'s.
type logFlags struct {
	lines int
	since string
}

// serviceLog prints a log the helper keeps (service.log): mihomo's
// output or the helper's own log, which only administrators can open
// directly on Windows and Linux. Several files get a header each.
func serviceLog(cf *clientFlags, name string, lf logFlags, now time.Time) error {
	if !slices.Contains(rpc.ServiceLogSources, name) {
		return fmt.Errorf("service log: want %s", strings.Join(rpc.ServiceLogSources, " or "))
	}
	p := rpc.ServiceLogParams{Name: name, Lines: lf.lines}
	if lf.since != "" {
		t, err := parseSince(lf.since, now)
		if err != nil {
			return err
		}
		p.Since = t
		if p.Lines == 0 {
			p.Lines = 2000 // a time bounds it; take what the helper gives
		}
	}
	var r rpc.ServiceLog
	return cf.callPrint(rpc.MethodServiceLog, p, &r, func() {
		for _, f := range r.Files {
			if len(r.Files) > 1 {
				fmt.Fprintf(stdout, "==> %s <==\n", f.File)
			}
			for _, l := range f.Lines {
				fmt.Fprintln(stdout, l)
			}
		}
	})
}

// parseSince reads --since: a duration back from now ("10m") or a time
// (RFC 3339).
func parseSince(s string, now time.Time) (time.Time, error) {
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return now.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("--since %q: want a duration (10m, 2h) or an RFC 3339 time", s)
}

func runAccessory(kind string, args []string) error {
	fs, cf := newFlagSet("kernel "+kind, fmt.Sprintf("usage: twinnel kernel %s check|update [--force]\n\n", kind))
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
			fmt.Fprintln(stdout, "mihomo reads the new files at its next reload: twinnel compose --apply --force (or restart the service)")
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
		fmt.Fprintln(w, "no health report yet (twinnel helper health --run)")
		return
	}
	fmt.Fprintf(w, "overall %s, %s (%s), reason %q\n", rep.Overall, r.At.Local().Format("15:04:05"), rep.Duration.Round(1e6), r.Reason)
	t := tw()
	for _, c := range rep.Checks {
		fmt.Fprintf(t, "%s\t%s\t%s\n", c.Status, c.ID, c.Summary)
	}
	t.Flush()
}

// runHelperEgress runs the egress test once in the helper (health.egress):
// one HTTPS request to an IP-echo address directly and one through mihomo.
// It prints the egress check alone; --json prints it as JSON.
func runHelperEgress(args []string) error {
	fs, cf := newFlagSet("helper egress", "usage: twinnel helper egress [--json]\n\nRun the egress test once: one HTTPS request to an IP-echo address directly\nand one through mihomo. The result stays in the helper's health report.\n\n")
	if _, done, err := parse(fs, args); done || err != nil {
		return err
	}
	var r rpc.HealthReport
	if err := cf.call(rpc.MethodHealthEgress, rpc.HealthEgressParams{}, &r); err != nil {
		return err
	}
	cr, err := egressCheck(r)
	if err != nil {
		return err
	}
	if cf.json {
		b, _ := json.MarshalIndent(cr, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return nil
	}
	printEgress(stdout, cr)
	return nil
}

// egressCheck is the egress check in a health.egress answer.
func egressCheck(r rpc.HealthReport) (health.CheckReport, error) {
	rep, err := healthsrc.Decode(r)
	if err != nil {
		return health.CheckReport{}, err
	}
	if rep != nil {
		for _, c := range rep.Checks {
			if c.ID == (health.LeakEgress{}).ID() {
				return c, nil
			}
		}
	}
	return health.CheckReport{}, errors.New("the helper's report has no egress check")
}

func printEgress(w io.Writer, cr health.CheckReport) {
	at := ""
	if !cr.At.IsZero() {
		at = ", " + cr.At.Local().Format("15:04:05")
	}
	fmt.Fprintf(w, "%s%s: %s\n", cr.Status, at, cr.Summary)
	for _, e := range health.TidyEvidence(cr.Evidence) {
		fmt.Fprintf(w, "  %s\n", e)
	}
}
