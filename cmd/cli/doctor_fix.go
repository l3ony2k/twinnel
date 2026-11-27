package main

// twinnel doctor --fix <check>: the fixes of a check's failure reasons
// (core/healthfix), the same the app's 修复 / 去调整 offer. It previews
// each one-click fix, asks before applying (unless --yes), runs the
// checks again through the helper and prints the check's new result and
// how to undo.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/healthfix"
	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/rpc/healthsrc"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// fixOptions are doctor's --fix flags.
type fixOptions struct {
	check, reason, undo, socket, lang string
	yes, json                         bool
	in                                io.Reader
	out                               io.Writer
	// fixer builds the Fixer (tests pass fakes); nil dials the helper.
	fixer func(ctx context.Context) (*healthfix.Fixer, func(), error)
}

func runDoctorFix(o fixOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if o.lang == "" {
		o.lang = health.DefaultLang
	}
	mk := o.fixer
	if mk == nil {
		mk = func(ctx context.Context) (*healthfix.Fixer, func(), error) { return dialFixer(ctx, o.socket, o.lang) }
	}
	f, done, err := mk(ctx)
	if err != nil {
		return err
	}
	defer done()
	w := o.out
	if o.undo != "" {
		var u healthfix.Undo
		if err := json.Unmarshal([]byte(o.undo), &u); err != nil {
			return fmt.Errorf("--undo: %w", err)
		}
		// Say what the undo does, and that it goes back to the failing
		// state, before it runs.
		up := f.UndoPreview(&u)
		fmt.Fprintf(w, "undo %s\n", u.Action)
		for _, s := range up.Steps {
			fmt.Fprintf(w, "  - %s\n", s)
		}
		fmt.Fprintf(w, "  ! %s\n", up.Warning)
		if !o.yes {
			fmt.Fprint(w, "  undo? [y/N] ")
			line, _ := bufio.NewReader(o.in).ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
				fmt.Fprintln(w, "  skipped")
				return nil
			}
		}
		rep, err := f.Undo(ctx, &u)
		if err != nil {
			return err
		}
		fmt.Fprintln(w, "undone")
		printCheck(w, rep, u.Action)
		return nil
	}
	if f.Helper == nil {
		return errors.New("--fix needs the helper")
	}
	var hr rpc.HealthReport
	if err := f.Helper.Call(ctx, rpc.MethodHealthRun, rpc.HealthRunParams{Reason: "cli", Lang: o.lang}, &hr); err != nil {
		return err
	}
	rep, err := healthsrc.Decode(hr)
	if err != nil || rep == nil {
		return fmt.Errorf("no health report: %v", err)
	}
	var cr *health.CheckReport
	for i := range rep.Checks {
		if rep.Checks[i].ID == o.check {
			cr = &rep.Checks[i]
		}
	}
	if cr == nil {
		return fmt.Errorf("no check %q (checks: %s)", o.check, strings.Join(health.DefaultRegistry().IDs(), ", "))
	}
	if len(cr.Reasons) == 0 {
		fmt.Fprintf(w, "%s: %s — %s\n", cr.ID, cr.Status, cr.Summary)
		if cr.Status == health.StatusWarn || cr.Status == health.StatusFail {
			return errors.New("the helper did not say why (an older helper?): upgrade it, then try again")
		}
		fmt.Fprintln(w, "nothing to fix")
		return nil
	}
	plans := f.Plans(ctx, *cr)
	if o.reason != "" {
		var keep []healthfix.Plan
		for _, p := range plans {
			if p.Reason == o.reason {
				keep = append(keep, p)
			}
		}
		if plans = keep; len(plans) == 0 {
			return fmt.Errorf("%s has no reason %q (reasons: %s)", cr.ID, o.reason, strings.Join(cr.Reasons, ", "))
		}
	}
	if o.json {
		b, _ := json.MarshalIndent(plans, "", "  ")
		fmt.Fprintln(w, string(b))
		if !o.yes {
			return nil
		}
	}
	fmt.Fprintf(w, "%s: %s — %s\n\n", cr.Title, cr.Status, cr.Summary)
	rd := bufio.NewReader(o.in)
	for _, p := range plans {
		fmt.Fprintf(w, "[%s] %s\n  %s\n  %s\n", p.Kind, p.Reason, p.Text.Plain, p.Text.How)
		if p.Command != "" && p.Kind == health.FixManual {
			fmt.Fprintf(w, "  $ %s\n", p.Command)
		}
		if p.Kind == health.FixGuided {
			fmt.Fprintf(w, "  → the app: %s\n", p.Target)
		}
		if p.Action == "" {
			fmt.Fprintln(w)
			continue
		}
		if p.Blocked != "" {
			fmt.Fprintf(w, "  cannot fix here: %s\n\n", p.Blocked)
			continue
		}
		for _, s := range p.Steps {
			fmt.Fprintf(w, "  - %s\n", s)
		}
		if !o.yes {
			fmt.Fprint(w, "  apply? [y/N] ")
			line, _ := rd.ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
				fmt.Fprintln(w, "  skipped")
				continue
			}
		}
		out, err := f.Apply(ctx, p)
		if err != nil {
			return fmt.Errorf("%s: %w", p.Action, err)
		}
		if out.Check != nil {
			fmt.Fprintf(w, "  now: %s — %s\n", out.Check.Status, out.Check.Summary)
		}
		for _, n := range out.Notes {
			fmt.Fprintf(w, "  note: %s\n", n)
		}
		if out.Undo != nil {
			fmt.Fprintf(w, "  undo: twinnel doctor --undo '%s'\n", undoArg(out.Undo))
		}
		fmt.Fprintln(w)
	}
	return nil
}

// undoArg is u as the --undo argument, safe in single quotes in sh and
// PowerShell alike: the JSON carries no quote of its own (a layer's
// content may hold one).
func undoArg(u *healthfix.Undo) string {
	b, _ := json.Marshal(u)
	return strings.ReplaceAll(string(b), "'", `\u0027`)
}

func printCheck(w io.Writer, rep *health.Report, _ string) {
	if rep == nil {
		return
	}
	fmt.Fprintf(w, "health: %s\n", rep.Overall)
}

// dialFixer connects to the helper, mihomo's controller (credentials from
// the helper) and Tailscale's LocalAPI.
func dialFixer(ctx context.Context, socket, lang string) (*healthfix.Fixer, func(), error) {
	c, err := rpc.Dial(ctx, nil, socket)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot reach the helper at %s: %w", socket, err)
	}
	f := &healthfix.Fixer{Helper: c, Lang: lang, Platform: runtime.GOOS}
	var cr rpc.ControllerCredentials
	if err := c.Call(ctx, rpc.MethodControllerCredentials, nil, &cr); err == nil && cr.Controller != "" {
		if mc, err := mihomo.New(mihomo.Options{Address: cr.Controller, Secret: cr.Secret, AllowWrites: true, Timeout: 10 * time.Second}); err == nil {
			f.Mihomo = healthfix.MihomoClient(mc)
		}
	}
	f.Tailscale = healthfix.TailscaleClient(tailscale.New(tailscale.DefaultAPI(), true))
	return f, func() { c.Close() }, nil
}
