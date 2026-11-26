package main

// Takeover over RPC: plans kept by the helper, adopt, controller.secure.

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/rpc"
)

func takeoverPlanViaHelper(socket, base string, asJSON bool) error {
	cf := &clientFlags{socket: socket, json: asJSON, timeout: 4 * time.Minute}
	var p rpc.TakeoverPlan
	return cf.callPrint(rpc.MethodTakeoverPlan, rpc.TakeoverPlanParams{Base: base}, &p, func() {
		fmt.Fprint(stdout, p.Summary)
		fmt.Fprintf(stdout, "\nplan id:  %s (kept by the helper, state %s)\n", p.PlanID, p.State)
		fmt.Fprintf(stdout, "next:     duet helper adopt %s\n", p.PlanID)
	})
}

func runHelperPlans(args []string) error {
	fs, cf := newFlagSet("helper plans", "usage: duet helper plans [PLAN_ID] [--json]\n\nLists the takeover plans the helper keeps; with PLAN_ID shows one in full.\n\n")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	params := rpc.TakeoverPlansParams{}
	if len(pos) > 0 {
		params.PlanID = pos[0]
	}
	var r []rpc.TakeoverPlan
	return cf.callPrint(rpc.MethodTakeoverPlans, params, &r, func() {
		if params.PlanID != "" && len(r) == 1 {
			printPlanHead(stdout, r[0])
			fmt.Fprint(stdout, "\n"+r[0].Summary)
			return
		}
		if len(r) == 0 {
			fmt.Fprintln(stdout, "no plans (make one with: duet takeover plan --via-helper)")
			return
		}
		w := tw()
		fmt.Fprintln(w, "PLAN\tSTATE\tCREATED\tEQUIVALENT\tLIVE")
		for _, p := range r {
			fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%s\n", p.PlanID, p.State, p.CreatedAt.Local().Format("2006-01-02 15:04"), p.Equivalent, p.LiveConfig)
		}
		w.Flush()
	})
}

func runHelperDiscard(args []string) error {
	fs, cf := newFlagSet("helper discard", "usage: duet helper discard PLAN_ID [--force]\n\n")
	var force bool
	fs.BoolVar(&force, "force", false, "also delete an adopted plan (it holds the backup of the live config)")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if err := needArgs(pos, 1, "duet helper discard PLAN_ID"); err != nil {
		return err
	}
	return cf.callPrint(rpc.MethodTakeoverDiscard, rpc.TakeoverDiscardParams{PlanID: pos[0], Force: force}, nil, func() {
		fmt.Fprintf(stdout, "discarded %s\n", pos[0])
	})
}

func printPlanHead(w io.Writer, p rpc.TakeoverPlan) {
	fmt.Fprintf(w, "plan %s (%s), made %s\n", p.PlanID, p.State, p.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "  live config  %s (sha256 %s)\n", p.LiveConfig, shortHash(p.LiveSHA256))
	fmt.Fprintf(w, "  base         %s\n", p.Base)
	fmt.Fprintf(w, "  mihomo       %s, service %s, controller %s (secret %s)\n", p.MihomoBinary, orNone(p.MihomoService), p.Controller, setWord(p.SecretSet))
	fmt.Fprintf(w, "  tailscale    service %s\n", orNone(p.TailscaleSvc))
	fmt.Fprintf(w, "  checks       equivalent=%v mihomo-t=%v\n", p.Equivalent, p.Validated)
	if p.Backup != "" {
		fmt.Fprintf(w, "  backup       %s\n", p.Backup)
	}
}

func setWord(b bool) string {
	if b {
		return "set"
	}
	return "empty"
}

const adoptSteps = `What adopt does (the helper runs every step; nothing is done yet):
  1. re-checks that %s is byte-identical to the plan (else: re-plan)
  2. copies it to the plan dir (live-backup.yaml) as the exact backup
  3. imports the base as subscription "adopted"%s and the layers %s;
     settings point at the live binary, config and controller; services %s
     and %s are marked adopted (their plists are never replaced);
     Tailscale coexist on, leak guard off (choose groups later)
  4. runs the health checks magicdns, tailnet-resolver-binding,
     tailscale-direct-rules, tailnet-route, system-dns
  5. writes the composed config (with the MagicDNS fix) and reloads mihomo
     (PUT /configs; the controller secret stays %s)
  6. runs the checks again; if any is worse, it writes the backup back,
     reloads, and undoes the import
`

func runHelperAdopt(args []string) error {
	fs, cf := newFlagSet("helper adopt", "usage: duet helper adopt PLAN_ID [--yes] [--skip-health]\n\nTakes over the running mihomo with a plan from `duet takeover plan --via-helper`.\n\n")
	var yes, skipHealth bool
	fs.BoolVar(&yes, "yes", false, "do not ask for confirmation")
	fs.BoolVar(&skipHealth, "skip-health", false, "adopt without the before/after health comparison (no automatic rollback)")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if err := needArgs(pos, 1, "duet helper adopt PLAN_ID"); err != nil {
		return err
	}
	var plans []rpc.TakeoverPlan
	if err := cf.call(rpc.MethodTakeoverPlans, rpc.TakeoverPlansParams{PlanID: pos[0]}, &plans); err != nil {
		return err
	}
	if len(plans) != 1 {
		return fmt.Errorf("plan %s not found", pos[0])
	}
	p := plans[0]
	if !cf.json || !yes {
		printPlanHead(stdout, p)
		for _, c := range p.Changes {
			tag := "UNEXPECTED"
			if c.Deliberate {
				tag = "deliberate"
			}
			fmt.Fprintf(stdout, "  [%s] %s: %s\n", tag, c.Area, c.What)
		}
		for _, w := range p.Warnings {
			fmt.Fprintf(stdout, "  warning: %s\n", w)
		}
		var ids []string
		for _, l := range p.Layers {
			if l.Stage == "base" || l.Stage == "user" {
				ids = append(ids, l.ID)
			}
		}
		url := ""
		if p.SubscriptionURL {
			url = " (its URL into the secret store)"
		}
		secret := "empty, as now"
		if p.SecretSet {
			secret = "as it is"
		}
		fmt.Fprintf(stdout, "\n"+adoptSteps, p.LiveConfig, url, strings.Join(ids, ", "), orNone(p.MihomoService), orNone(p.TailscaleSvc), secret)
		if skipHealth {
			fmt.Fprintln(stdout, "  --skip-health: steps 4 and 6 are skipped, there is no automatic rollback")
		}
	}
	if !p.Equivalent {
		return errors.New("the plan has UNEXPECTED differences; the helper will refuse it. Review and re-plan")
	}
	if !yes && !confirm() {
		return errors.New("adopt cancelled")
	}
	var r rpc.AdoptResult
	cf.timeout = max(cf.timeout, 6*time.Minute)
	err = cf.callPrint(rpc.MethodTakeoverAdopt, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash, SkipHealth: skipHealth}, &r, func() {
		printAdopt(stdout, r)
	})
	if err != nil {
		return err
	}
	if !r.OK {
		return fmt.Errorf("adopt did not complete: %s", r.Error)
	}
	return nil
}

func printAdopt(w io.Writer, r rpc.AdoptResult) {
	fmt.Fprintf(w, "\nadopt %s\n", r.PlanID)
	for i, s := range r.Steps {
		fmt.Fprintf(w, "  %d. %-14s %-7s %s\n", i+1, s.Name, s.Status, s.Detail)
	}
	printChecks := func(title string, cs []rpc.AdoptCheck) {
		if len(cs) == 0 {
			return
		}
		fmt.Fprintf(w, "%s\n", title)
		for _, c := range cs {
			fmt.Fprintf(w, "  %-5s %-25s %s\n", c.Status, c.ID, truncate(c.Summary, 110))
		}
	}
	printChecks("health before", r.HealthBefore)
	printChecks("health after", r.HealthAfter)
	printChecks("health after rollback", r.HealthAfterRollback)
	for _, x := range r.Warnings {
		fmt.Fprintf(w, "warning: %s\n", x)
	}
	if r.HistoryID != "" {
		fmt.Fprintf(w, "history:     %s (applied), backup %s\n", r.HistoryID, orNone(r.BackupHistoryID))
	}
	if r.Backup != "" {
		fmt.Fprintf(w, "exact backup %s\n", r.Backup)
	}
	switch {
	case r.OK:
		fmt.Fprintln(w, "result:      adopted")
	case r.RolledBack:
		fmt.Fprintf(w, "result:      ROLLED BACK: %s\n", r.Error)
	default:
		fmt.Fprintf(w, "result:      FAILED: %s\n", r.Error)
	}
}

func runHelperSecure(args []string) error {
	fs, cf := newFlagSet("helper secure-controller", "usage: duet helper secure-controller [--yes]\n\nGenerates a new controller secret, keeps it in the helper's secret store\n(macOS: System keychain) and applies it. Clients of the controller (the web\ndashboard, scripts) then need the new secret.\n\n")
	var yes bool
	fs.BoolVar(&yes, "yes", false, "do not ask for confirmation")
	if _, done, err := parse(fs, args); done || err != nil {
		return err
	}
	if !yes {
		fmt.Fprintln(stdout, "This sets a new external-controller secret and reloads mihomo.")
		if !confirm() {
			return errors.New("cancelled")
		}
	}
	var r rpc.ControllerSecureResult
	return cf.callPrint(rpc.MethodControllerSecure, nil, &r, func() {
		if r.Apply != nil {
			fmt.Fprintf(stdout, "applied: written=%v reloaded=%v history %s\n", r.Apply.Written, r.Apply.Reloaded, r.Apply.HistoryID)
		}
		fmt.Fprintf(stdout, "secret %s\n", map[bool]string{true: "replaced", false: "set (there was none)"}[r.Replaced])
		for _, x := range r.Warnings {
			fmt.Fprintf(stdout, "note: %s\n", x)
		}
	})
}
