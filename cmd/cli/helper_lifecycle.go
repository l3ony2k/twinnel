package main

// duet helper uninstall / upgrade / restart: the helper's own lifecycle.
// Each runs as root the same way as install (one administrator prompt
// through osascript), and none of them restarts or reloads mihomo or
// tailscaled, except the documented move back of a hardened mihomo that
// uninstall --restore-mihomo asks for.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/core/store"
	"github.com/l3ony2k/duet/core/uninstall"
	"github.com/l3ony2k/duet/core/version"
)

// Replaceable in tests (no test may reach a real helper, mihomo or binary
// outside its temp dir).
var (
	// helperRPC calls the helper at socket with a short timeout.
	helperRPC = func(ctx context.Context, socket, method string, params, result any) error {
		c, err := rpc.Dial(ctx, nil, socket)
		if err != nil {
			return err
		}
		defer c.Close()
		return c.Call(ctx, method, params, result)
	}
	// binaryVersion runs `<bin> --version` ("duet-helper 1.2.3").
	binaryVersion = func(ctx context.Context, bin string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin, "--version").Output()
		if err != nil {
			return "", fmt.Errorf("%s --version: %w", bin, err)
		}
		f := strings.Fields(string(out))
		if len(f) < 2 {
			return "", fmt.Errorf("%s --version: unexpected output %q", bin, strings.TrimSpace(string(out)))
		}
		return f[len(f)-1], nil
	}
	// controllerUp reports whether mihomo's controller answers HTTP at
	// all (401 counts: it runs, with a secret).
	controllerUp = func(ctx context.Context, addr string) bool {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/version", nil)
		if err != nil {
			return false
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	}
	pollEvery = 500 * time.Millisecond
	// fileExistsFn and readFile see the real filesystem.
	fileExistsFn = func(p string) bool { _, err := os.Stat(p); return err == nil }
)

// ---- uninstall ----

const uninstallUsage = `usage: duet helper uninstall [--purge] [--restore-mihomo | --keep-mihomo] [--dry-run] [--yes] [--json]

Removes the helper (its service, binary and socket). Lists exactly what goes
and what stays first.

--purge           also delete duet's data: the state dir (settings,
                  subscriptions, layers, history, logs, backups, kernels)
                  and the keychain items.
--restore-mihomo  after "duet helper harden": move mihomo back to the
                  service it ran under before (a few seconds offline),
                  then remove the helper.
--keep-mihomo     after "duet helper harden": leave mihomo running from
                  duet's LaunchDaemon, unmanaged (not with --purge).

`

func runHelperUninstall(args []string) error {
	fs, f := newInstallFlags("helper uninstall", false)
	var o uninstall.Options
	var asJSON bool
	fs.BoolVar(&o.Purge, "purge", false, "also delete duet's data (state dir and keychain items)")
	fs.BoolVar(&o.RestoreMihomo, "restore-mihomo", false, "after harden: move mihomo back to its original service first")
	fs.BoolVar(&o.KeepMihomo, "keep-mihomo", false, "after harden: leave mihomo running from duet's LaunchDaemon")
	fs.BoolVar(&asJSON, "json", false, "with --dry-run: print the plan (or the refusal) as JSON")
	fs.Usage = func() { fmt.Fprint(fs.Output(), uninstallUsage); fs.PrintDefaults() }
	if _, done, err := parse(fs, args); done || err != nil {
		return err
	}
	for _, x := range []struct {
		on   bool
		flag string
	}{{o.Purge, "--purge"}, {o.RestoreMihomo, "--restore-mihomo"}, {o.KeepMihomo, "--keep-mihomo"}} {
		if x.on {
			f.extra = append(f.extra, x.flag)
		}
	}
	facts := uninstallFacts(f)
	plan, err := uninstall.Make(facts, o)
	if asJSON {
		out := struct {
			Plan    *uninstall.Plan    `json:"plan,omitempty"`
			Refusal *uninstall.Refusal `json:"refusal,omitempty"`
		}{}
		var r *uninstall.Refusal
		switch {
		case errors.As(err, &r):
			out.Refusal = r
		case err != nil:
			return err
		default:
			out.Plan = &plan
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(b))
		if !f.dryRun {
			return errors.New("--json prints the plan only; run again without it to uninstall")
		}
		return nil
	}
	if err != nil {
		return err
	}
	steps, err := planHelperUninstall(f.stateDir, f.socket, f.label)
	if err != nil {
		return err
	}
	purge, err := purgeSteps(plan, f.stateDir)
	if err != nil {
		return err
	}
	show := func(w io.Writer) { printUninstall(w, plan, steps, purge) }
	return runAsRoot("uninstall", f, show, func(ctx context.Context) error {
		return runUninstall(ctx, plan, steps, purge, f.stateDir)
	}, nil)
}

// uninstallFacts reads what uninstalling must take care of: as root from
// the helper's settings.json, else from the running helper (helper.info).
// Unknown (nil Info) when neither works; the elevated run checks again.
func uninstallFacts(f *installFlags) uninstall.Facts {
	bin, def := helperPaths(f.stateDir, f.label)
	facts := uninstall.Facts{
		HelperService: def, HelperBinary: bin, Socket: f.socket, StateDir: f.stateDir,
		SecretStore: secretStore,
	}
	if geteuid() == 0 {
		facts.OldPlistExists = fileExistsFn
		facts.Info = infoFromSettings(f.stateDir)
		return facts
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var info rpc.HelperInfo
	if err := helperRPC(ctx, f.socket, rpc.MethodHelperInfo, nil, &info); err == nil {
		facts.Info = &info
		facts.OldPlistExists = fileExistsFn // /Library/LaunchDaemons is readable
	}
	return facts
}

// infoFromSettings builds helper.info's answer from settings.json (as root:
// the file is 0600). A missing file is a fresh helper: the defaults.
func infoFromSettings(stateDir string) *rpc.HelperInfo {
	st := &store.Settings{}
	if b, err := os.ReadFile(filepath.Join(stateDir, "settings.json")); err == nil {
		if json.Unmarshal(b, st) != nil {
			return nil // unreadable: unknown, never guess
		}
	} else if !os.IsNotExist(err) {
		return nil
	}
	st.Defaults(stateDir)
	info := &rpc.HelperInfo{
		StateDir: stateDir,
		Label:    st.HelperLabel,
		Mihomo: rpc.HelperMihomoService{
			Label: st.Mihomo.ServiceLabel, Adopted: st.Mihomo.ServiceAdopted, Controller: st.Mihomo.Controller,
			Installed: fileExistsFn(serviceDefinition(st.Mihomo.ServiceLabel)),
		},
		SecretKeys:    st.SecretKeys(),
		Subscriptions: len(st.Subscriptions),
		Layers:        len(st.Layers),
	}
	if h := st.Hardened; h != nil {
		info.Hardened = &rpc.HelperHardened{PlanID: h.PlanID, OldLabel: h.OldLabel, OldPlist: h.OldPlist}
	}
	return info
}

// purgeSteps delete the keychain items and the state dir. The state dir is
// removed with rm -rf, so it must look like duet's: absolute, not a
// top-level dir, named duet.
func purgeSteps(p uninstall.Plan, stateDir string) ([]service.Step, error) {
	var steps []service.Step
	for _, it := range p.Remove {
		switch it.Kind {
		case uninstall.Secrets:
			keys := it.Names
			if len(keys) == 0 {
				keys = []string{store.SecretController} // unknown: at least the one every helper may have
			}
			steps = append(steps, planSecretsDelete(keys)...)
		case uninstall.StateDir:
			dir := filepath.Clean(it.Path)
			if !filepath.IsAbs(dir) || filepath.Dir(dir) == "/" || !strings.Contains(strings.ToLower(filepath.Base(dir)), "duet") {
				return nil, fmt.Errorf("refusing to delete %q: not duet's state dir", it.Path)
			}
			steps = append(steps, service.Step{Kind: service.StepRun, Argv: []string{"/bin/rm", "-rf", "--", dir}})
		}
	}
	return steps, nil
}

// runUninstall runs as root: the move back of a hardened mihomo first
// (stop duet's, wait until its controller is gone, start the original,
// wait until it answers; start duet's again if it does not), then the
// helper's removal, then the purge.
func runUninstall(ctx context.Context, p uninstall.Plan, steps, purge []service.Step, stateDir string) error {
	if r := p.Restore; r != nil {
		if err := restoreMihomo(ctx, r, p.Removes(uninstall.StateDir), stateDir); err != nil {
			return err
		}
	}
	if err := applyPlan(ctx, steps); err != nil {
		return err
	}
	return applyPlan(ctx, purge)
}

// restoreTimeouts bound the waits of restoreMihomo (runbook 8c; the same
// limits as harden's cut).
var restoreTimeouts = struct{ down, up time.Duration }{15 * time.Second, 45 * time.Second}

func restoreMihomo(ctx context.Context, r *uninstall.Restore, purge bool, stateDir string) error {
	stop, start, undo := planMihomoRestore(r.FromLabel, r.ToLabel, r.ToPlist)
	if stop == nil {
		return errors.New("moving mihomo back is only implemented for macOS")
	}
	fmt.Fprintf(stdout, "# mihomo: stopping %s (the network is down until %s answers)\n", r.FromLabel, r.ToLabel)
	if err := applyPlan(ctx, stop); err != nil {
		return err
	}
	if !waitController(ctx, r.Controller, false, restoreTimeouts.down) {
		fmt.Fprintf(stdout, "# mihomo: %s still answers after %s; starting %s anyway\n", r.Controller, restoreTimeouts.down, r.ToLabel)
	}
	startErr := applyPlan(ctx, start)
	if startErr == nil && waitController(ctx, r.Controller, true, restoreTimeouts.up) {
		fmt.Fprintf(stdout, "# mihomo: %s answers at %s\n", r.ToLabel, r.Controller)
		// duet's plist goes; without --purge a copy stays in the state dir.
		def := serviceDefinition(r.FromLabel)
		var after []service.Step
		if !purge {
			after = append(after, service.Step{Kind: service.StepCopy, Src: def, Path: filepath.Join(stateDir, "harden", r.FromLabel+".plist"), Mode: 0o644})
		}
		after = append(after, service.Step{Kind: service.StepRemove, Path: def})
		return applyPlan(ctx, after)
	}
	why := "did not answer within " + restoreTimeouts.up.String()
	if startErr != nil {
		why = startErr.Error()
	}
	fmt.Fprintf(stdout, "# mihomo: %s %s; starting %s again\n", r.ToLabel, why, r.FromLabel)
	if err := applyPlan(ctx, undo); err != nil {
		return fmt.Errorf("%s did not start (%s), and starting %s again failed too: %w; run `bash ~/mihomo-migration/emergency-restore.sh` or see runbook step 8c", r.ToLabel, why, r.FromLabel, err)
	}
	waitController(ctx, r.Controller, true, restoreTimeouts.up)
	return fmt.Errorf("%s did not start (%s); %s runs again and the helper was not removed", r.ToLabel, why, r.FromLabel)
}

// waitController polls until the controller answers (up) or stops
// answering (!up), at most d.
func waitController(ctx context.Context, addr string, up bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if controllerUp(ctx, addr) == up {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		time.Sleep(pollEvery)
	}
}

func printUninstall(w io.Writer, p uninstall.Plan, steps, purge []service.Step) {
	fmt.Fprintln(w, "removes:")
	for _, it := range p.Remove {
		fmt.Fprintf(w, "  - %s\n", describeItem(it))
	}
	fmt.Fprintln(w, "keeps:")
	for _, it := range p.Keep {
		fmt.Fprintf(w, "  - %s\n", describeItem(it))
	}
	if !p.Known {
		fmt.Fprintln(w, "# The helper's settings could not be read (it is not running). The run as root reads them and stops if mihomo depends on the helper.")
	}
	fmt.Fprintln(w, "steps:")
	n := 0
	if r := p.Restore; r != nil {
		stop, start, undo := planMihomoRestore(r.FromLabel, r.ToLabel, r.ToPlist)
		for _, s := range stop {
			n++
			fmt.Fprintf(w, "%d. %s\n", n, s)
		}
		n++
		fmt.Fprintf(w, "%d. wait until %s stops answering (at most %s)\n", n, r.Controller, restoreTimeouts.down)
		for _, s := range start {
			n++
			fmt.Fprintf(w, "%d. %s\n", n, s)
		}
		n++
		fmt.Fprintf(w, "%d. wait until %s answers (at most %s); if it does not:\n", n, r.Controller, restoreTimeouts.up)
		for _, s := range undo {
			fmt.Fprintf(w, "     %s\n", s)
		}
		fmt.Fprintln(w, "     and stop: nothing else is removed")
		n++
		fmt.Fprintf(w, "%d. rm -f %s\n", n, serviceDefinition(r.FromLabel))
	}
	for _, s := range append(append([]service.Step{}, steps...), purge...) {
		n++
		fmt.Fprintf(w, "%d. %s\n", n, s)
	}
}

func describeItem(it uninstall.Item) string {
	switch it.Kind {
	case uninstall.HelperService:
		return "helper service " + it.Path
	case uninstall.HelperBinary:
		return "helper binary " + it.Path
	case uninstall.HelperSocket:
		return "helper socket " + it.Path
	case uninstall.StateDir:
		s := "duet's data " + it.Path + " (settings, subscriptions, layers, history, logs, backups, kernels)"
		if it.Subscriptions > 0 || it.Layers > 0 {
			s += fmt.Sprintf(": %d subscriptions, %d layers", it.Subscriptions, it.Layers)
		}
		return s
	case uninstall.Secrets:
		if len(it.Names) == 0 {
			return "keychain items duet.*"
		}
		names := make([]string, len(it.Names))
		for i, n := range it.Names {
			names[i] = "duet." + n
		}
		return "keychain items " + strings.Join(names, ", ")
	case uninstall.DuetMihomo:
		return "duet's mihomo service " + it.Path
	case uninstall.OriginalMihomo:
		return "mihomo's original service " + it.Path + " (and its binary and config)"
	case uninstall.Tailscale:
		return "Tailscale (not touched)"
	case uninstall.App:
		return "duet.app and the duet command"
	}
	return string(it.Kind) + " " + it.Path
}

// ---- upgrade ----

const upgradeUsage = `usage: duet helper upgrade [--check] [--force] [--dry-run] [--yes] [--json]

Replaces the installed helper with the one next to this duet (or
--helper-bin) when they differ, restarts only the helper's service and
waits until it answers with the new version. mihomo and tailscaled are not
touched. Refuses to install an older helper unless --force.

`

// UpgradeCheck is `duet helper upgrade --check --json`.
type UpgradeCheck struct {
	Bundled     string `json:"bundled"`                // version of the helper next to duet
	BundledPath string `json:"bundled_path"`           //
	Installed   string `json:"installed,omitempty"`    // version of the installed binary
	Running     string `json:"running,omitempty"`      // version the running helper reports
	Action      string `json:"action"`                 // see the Upgrade* constants
	Detail      string `json:"detail,omitempty"`       //
	InstalledAt string `json:"installed_at,omitempty"` // path
}

// Upgrade actions.
const (
	UpgradeNone         = "none"          // same binary, running
	UpgradeInstall      = "upgrade"       // the bundled helper is newer (or another build)
	UpgradeRestart      = "restart"       // the binary is current, the running process is not
	UpgradeDowngrade    = "downgrade"     // the bundled helper is older: --force
	UpgradeNotInstalled = "not-installed" // duet helper install
)

func runHelperUpgrade(args []string) error {
	fs, f := newInstallFlags("helper upgrade", true)
	var check, force, asJSON bool
	fs.BoolVar(&check, "check", false, "only compare the versions")
	fs.BoolVar(&force, "force", false, "install the bundled helper even when it is older")
	fs.BoolVar(&asJSON, "json", false, "print as JSON: with --check the versions, else the result (OpResult)")
	fs.Usage = func() { fmt.Fprint(fs.Output(), upgradeUsage); fs.PrintDefaults() }
	if _, done, err := parse(fs, args); done || err != nil {
		return err
	}
	if f.helperBin == "" {
		f.helperBin = defaultHelperBin()
	}
	bin, err := filepath.Abs(f.helperBin)
	if err != nil {
		return err
	}
	f.helperBin = bin
	if force {
		f.extra = append(f.extra, "--force")
	}
	ctx := context.Background()
	c, err := checkUpgrade(ctx, f)
	if err != nil {
		return err
	}
	if check {
		if asJSON {
			b, _ := json.MarshalIndent(c, "", "  ")
			fmt.Fprintln(stdout, string(b))
			return nil
		}
		printUpgradeCheck(stdout, c)
		return nil
	}
	return jsonRun("upgrade", asJSON, f.dryRun, func() error { return helperUpgrade(ctx, f, c, force) })
}

func helperUpgrade(ctx context.Context, f *installFlags, c UpgradeCheck, force bool) error {
	var err error
	if !f.asRoot {
		printUpgradeCheck(stdout, c)
	}
	var steps []service.Step
	switch c.Action {
	case UpgradeNone:
		return nil
	case UpgradeNotInstalled:
		return errors.New("the helper is not installed: duet helper install")
	case UpgradeDowngrade:
		if !force {
			return fmt.Errorf("the bundled helper %s is older than the installed %s; pass --force to install it anyway", c.Bundled, c.Installed)
		}
		fallthrough
	case UpgradeInstall:
		// The install plan: copy the binary (written next to it, then
		// renamed over it) and reload the helper's own service definition,
		// which restarts only the helper.
		steps, err = planHelperInstall(f.helperBin, f.stateDir, f.socket, f.label)
	case UpgradeRestart:
		steps, err = planHelperRestart(f.label)
	}
	if err != nil {
		return err
	}
	if err := runVerified("upgrade", f, steps, c.Bundled); err != nil {
		return err
	}
	if f.asRoot || f.dryRun {
		return nil
	}
	// The elevated copy verified already; this one sees the same socket
	// and confirms (quickly) for the person who ran it.
	return verifyHelper(ctx, f.socket, c.Bundled, 0)
}

// runVerified is runPlan for the steps that restart the helper: as root,
// after the steps (or after they fail), it waits for the helper to answer
// with version want ("" any) and, when it does not, starts its service
// again (planHelperRestart: kickstart when loaded, bootstrap when not)
// and waits once more. A helper left unloaded is the one failure the
// person cannot fix from the app without a password, so root fixes it
// here.
func runVerified(op string, f *installFlags, steps []service.Step, want string) error {
	return runAsRoot(op, f, func(w io.Writer) { service.FormatPlan(w, steps) }, func(ctx context.Context) error {
		applyErr := applyPlan(ctx, steps)
		var verifyErr error
		if applyErr == nil {
			if verifyErr = verifyHelper(ctx, f.socket, want, 0); verifyErr == nil {
				return nil
			}
			fmt.Fprintf(stdout, "# the helper does not answer (%v); starting its service again\n", verifyErr)
		} else {
			fmt.Fprintf(stdout, "# %v; starting the helper's service again\n", applyErr)
		}
		recErr := recoverHelper(ctx, f, want)
		switch {
		case recErr == nil && applyErr == nil:
			return nil
		case recErr == nil:
			fmt.Fprintln(stdout, "# the helper runs again after the failed step")
			return nil
		case applyErr != nil:
			return fmt.Errorf("%w; starting it again failed too: %v", applyErr, recErr)
		default:
			return fmt.Errorf("%v; starting it again failed too: %v", verifyErr, recErr)
		}
	}, nil)
}

// recoverHelper starts the helper's service again (as root) and waits
// for it to answer.
func recoverHelper(ctx context.Context, f *installFlags, want string) error {
	steps, err := planHelperRestart(f.label)
	if err != nil {
		return err
	}
	for _, s := range steps {
		fmt.Fprintf(stdout, "#   %s\n", s)
	}
	if err := applyPlan(ctx, steps); err != nil {
		return err
	}
	return verifyHelper(ctx, f.socket, want, 0)
}

// checkUpgrade compares the bundled helper with the installed one and the
// running one. Reads only: `--version` of both binaries, their bytes, and
// status.get over the socket.
func checkUpgrade(ctx context.Context, f *installFlags) (UpgradeCheck, error) {
	c := UpgradeCheck{BundledPath: f.helperBin}
	if !fileExistsFn(f.helperBin) {
		return c, fmt.Errorf("helper binary %s not found (build it with `go build -o %s ./cmd/helper` or pass --helper-bin)", f.helperBin, f.helperBin)
	}
	var err error
	if c.Bundled, err = binaryVersion(ctx, f.helperBin); err != nil {
		return c, err
	}
	installed, _ := helperPaths(f.stateDir, f.label)
	c.InstalledAt = installed
	if !fileExistsFn(installed) {
		c.Action = UpgradeNotInstalled
		return c, nil
	}
	if c.Installed, err = binaryVersion(ctx, installed); err != nil {
		c.Detail = err.Error()
	}
	sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	var st rpc.Status
	if helperRPC(sctx, f.socket, rpc.MethodStatus, nil, &st) == nil {
		c.Running = st.HelperVersion
	}
	cancel()
	if sameFile(f.helperBin, installed) {
		c.Action = UpgradeNone
		if c.Running != "" && c.Running != c.Installed {
			c.Action, c.Detail = UpgradeRestart, "the installed binary is current; the running helper is not"
		}
		return c, nil
	}
	if cmp, ok := version.Compare(c.Bundled, c.Installed); ok && cmp < 0 {
		c.Action = UpgradeDowngrade
		return c, nil
	} else if ok && cmp == 0 {
		c.Detail = "same version, another build"
	}
	c.Action = UpgradeInstall
	return c, nil
}

// sameFile reports whether two files have the same bytes.
var sameFile = func(a, b string) bool {
	ha, err1 := fileHash(a)
	hb, err2 := fileHash(b)
	return err1 == nil && err2 == nil && ha == hb
}

func fileHash(p string) ([32]byte, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}

func printUpgradeCheck(w io.Writer, c UpgradeCheck) {
	fmt.Fprintf(w, "bundled   %s (%s)\n", c.Bundled, c.BundledPath)
	if c.Action != UpgradeNotInstalled {
		fmt.Fprintf(w, "installed %s (%s)\n", orDash(c.Installed), c.InstalledAt)
		fmt.Fprintf(w, "running   %s\n", orDash(c.Running))
	}
	text := map[string]string{
		UpgradeNone:         "up to date: nothing to do",
		UpgradeInstall:      "the installed helper will be replaced",
		UpgradeRestart:      "the helper will be restarted",
		UpgradeDowngrade:    "the bundled helper is OLDER than the installed one (--force to install it anyway)",
		UpgradeNotInstalled: "not installed (duet helper install)",
	}[c.Action]
	if c.Detail != "" {
		text += " (" + c.Detail + ")"
	}
	fmt.Fprintf(w, "action    %s\n", text)
}

// verifyTimeout bounds the wait for the helper to come back.
var verifyTimeout = 30 * time.Second

// verifyHelper waits until the helper answers status.get with version want
// (any version when want is ""), from a process other than oldPID when
// that is set.
func verifyHelper(ctx context.Context, socket, want string, oldPID int) error {
	deadline := time.Now().Add(verifyTimeout)
	var last string
	for {
		sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		var st rpc.Status
		err := helperRPC(sctx, socket, rpc.MethodStatus, nil, &st)
		cancel()
		if err == nil && (oldPID == 0 || st.PID != oldPID) {
			if want == "" || st.HelperVersion == want {
				fmt.Fprintf(stdout, "# helper %s answers (pid %d)\n", st.HelperVersion, st.PID)
				return nil
			}
			last = "it answers with version " + st.HelperVersion
		} else if err != nil {
			last = err.Error()
		} else {
			last = "the old process still answers"
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the helper did not come back within %s (%s); see its log, %s/logs/helper.log", verifyTimeout, last, store.DefaultDir)
		}
		time.Sleep(pollEvery)
	}
}

// ---- restart ----

func runHelperRestart(args []string) error {
	fs, f := newInstallFlags("helper restart", false)
	asJSON := fs.Bool("json", false, "print the result as JSON (OpResult)")
	if _, done, err := parse(fs, args); done || err != nil {
		return err
	}
	return jsonRun("restart", *asJSON, f.dryRun, func() error { return helperRestart(f) })
}

func helperRestart(f *installFlags) error {
	ctx := context.Background()
	if !f.asRoot && !f.dryRun {
		// A running helper restarts itself (helper.restart: it exits and
		// its service manager starts it again): no password needed.
		sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		var r rpc.HelperRestartResult
		err := helperRPC(sctx, f.socket, rpc.MethodHelperRestart, nil, &r)
		cancel()
		if err == nil {
			fmt.Fprintf(stdout, "# helper (pid %d) is restarting\n", r.PID)
			return verifyHelper(ctx, f.socket, "", r.PID)
		}
		fmt.Fprintf(stdout, "# the helper cannot restart itself (%s); restarting its service as root\n", shortErr(err))
	}
	steps, err := planHelperRestart(f.label)
	if err != nil {
		return err
	}
	if err := runVerified("restart", f, steps, ""); err != nil {
		return err
	}
	if f.asRoot || f.dryRun {
		return nil
	}
	return verifyHelper(ctx, f.socket, "", 0)
}

func shortErr(err error) string {
	var re *rpc.Error
	if errors.As(err, &re) {
		return re.Message
	}
	return err.Error()
}
