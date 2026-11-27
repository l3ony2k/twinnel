package main

// duet helper install / uninstall. The plan (copy the binary, write and
// load the LaunchDaemon) is printed first. Run as a normal user, duet
// re-executes itself as root through
//
//	osascript -e 'do shell script "<duet> helper install --as-root ..." with administrator privileges'
//
// which shows the system's administrator password dialog once. As root
// (--as-root) it applies the plan.

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/core/store"
)

// Replaceable in tests: nothing in a test may run osascript, launchctl or
// write outside its temp dir.
var (
	geteuid     = os.Geteuid
	executable  = os.Executable
	runElevated = func(ctx context.Context, argv []string, stdout, stderr io.Writer) error {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Stdout, cmd.Stderr = stdout, stderr
		return cmd.Run()
	}
	applyPlan = func(ctx context.Context, steps []service.Step) error {
		return service.Apply(ctx, steps, service.ExecExecutor{Timeout: time.Minute}, service.OSFiles{})
	}
	stdin  io.Reader = os.Stdin
	stdout io.Writer = os.Stdout
)

type installFlags struct {
	dryRun, yes, asRoot                bool
	helperBin, stateDir, socket, label string
	// extra are more flags for the elevated copy (uninstall --purge, ...).
	extra []string
}

func newInstallFlags(name string, withBin bool) (*flag.FlagSet, *installFlags) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	f := &installFlags{}
	fs.BoolVar(&f.dryRun, "dry-run", false, "print the plan (plist and commands); change nothing")
	fs.BoolVar(&f.yes, "yes", false, "do not ask for confirmation (the administrator password dialog still appears)")
	fs.BoolVar(&f.asRoot, "as-root", false, "internal: run the plan; only valid as root")
	if withBin {
		fs.StringVar(&f.helperBin, "helper-bin", "", "duet-helper `BINARY` to install (default: next to this duet binary)")
	}
	fs.StringVar(&f.stateDir, "state-dir", store.DefaultDir, "helper state `DIR`")
	fs.StringVar(&f.socket, "socket", rpc.DefaultSocket, "socket `PATH` the helper listens on")
	fs.StringVar(&f.label, "label", store.DefaultHelperLabel, "launchd `LABEL`")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: duet %s [flags]\n\nflags:\n", name)
		fs.PrintDefaults()
	}
	return fs, f
}

// rootArgs are the flags passed to the elevated copy of duet: every path
// absolute, so the root shell's working directory does not matter.
func (f *installFlags) rootArgs(sub string) ([]string, error) {
	args := []string{"helper", sub, "--as-root", "--yes"}
	if f.helperBin != "" {
		bin, err := filepath.Abs(f.helperBin)
		if err != nil {
			return nil, err
		}
		args = append(args, "--helper-bin", bin)
	}
	for _, p := range []string{f.stateDir, f.socket} {
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("%q must be an absolute path", p)
		}
	}
	args = append(args, "--state-dir", f.stateDir, "--socket", f.socket, "--label", f.label)
	return append(args, f.extra...), nil
}

func runHelperInstall(args []string) error {
	fs, f := newInstallFlags("helper install", true)
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
	steps, err := planHelperInstall(f.helperBin, f.stateDir, f.socket, f.label)
	if err != nil {
		return err
	}
	return runPlan("install", f, steps, func() error {
		if fi, err := os.Stat(f.helperBin); err != nil || fi.IsDir() {
			return fmt.Errorf("helper binary %s not found (build it with `go build -o %s ./cmd/helper` or pass --helper-bin)", f.helperBin, f.helperBin)
		}
		return nil
	})
}

// runPlan prints the plan, then runs it as root (directly with --as-root,
// else by re-executing duet through osascript).
func runPlan(op string, f *installFlags, steps []service.Step, precheck func() error) error {
	return runAsRoot(op, f, func(w io.Writer) { service.FormatPlan(w, steps) }, func(ctx context.Context) error {
		return applyPlan(ctx, steps)
	}, precheck)
}

// runAsRoot shows what an operation does (show), then runs it as root
// (run): directly with --as-root or under sudo, else by re-executing duet
// through osascript with the same flags (f.rootArgs), which asks for the
// administrator password once.
func runAsRoot(op string, f *installFlags, show func(io.Writer), run func(context.Context) error, precheck func() error) error {
	if f.asRoot {
		if geteuid() != 0 {
			return errors.New("--as-root is only for the elevated copy of duet; run without it")
		}
		fmt.Fprintf(stdout, "# duet helper %s: running as root\n", op)
		if precheck != nil {
			if err := precheck(); err != nil {
				return err
			}
		}
		if err := run(context.Background()); err != nil {
			return fmt.Errorf("helper %s failed: %w", op, err)
		}
		fmt.Fprintf(stdout, "# helper %s: done\n", op)
		return nil
	}

	if f.dryRun {
		fmt.Fprintf(stdout, "# duet helper %s --dry-run: nothing below has been run.\n", op)
	} else {
		fmt.Fprintf(stdout, "# duet helper %s will run these steps as root:\n", op)
	}
	show(stdout)
	if f.dryRun {
		return nil
	}
	if precheck != nil {
		if err := precheck(); err != nil {
			return err
		}
	}
	if geteuid() == 0 {
		// Already root (sudo): no dialog needed.
		if !f.yes && !confirm() {
			return errors.New("cancelled; nothing was changed")
		}
		if err := run(context.Background()); err != nil {
			return fmt.Errorf("helper %s failed: %w", op, err)
		}
		fmt.Fprintf(stdout, "# helper %s: done\n", op)
		return nil
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	rargs, err := f.rootArgs(op)
	if err != nil {
		return err
	}
	argv, err := osascriptArgv(exe, rargs)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "# macOS will ask for an administrator password, then run:")
	fmt.Fprintf(stdout, "#   %s\n", shellCommand(exe, rargs))
	if !f.yes && !confirm() {
		return errors.New("cancelled; nothing was changed")
	}
	if err := runElevated(context.Background(), argv, stdout, os.Stderr); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			// osascript exits 1 also when the user cancels the dialog
			// ("User canceled. (-128)" on stderr).
			return fmt.Errorf("helper %s did not complete (cancelled, or the elevated run failed; see above)", op)
		}
		return fmt.Errorf("helper %s: %w", op, err)
	}
	return nil
}

func confirm() bool {
	if fi, ok := stdin.(*os.File); ok {
		if st, err := fi.Stat(); err != nil || st.Mode()&os.ModeCharDevice == 0 {
			fmt.Fprintln(stdout, "# not a terminal: pass --yes to proceed")
			return false
		}
	}
	fmt.Fprint(stdout, "Proceed? [y/N] ")
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// shQuote quotes one word for /bin/sh.
func shQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=@%+,", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shellCommand(exe string, args []string) string {
	words := []string{shQuote(exe)}
	for _, a := range args {
		words = append(words, shQuote(a))
	}
	return strings.Join(words, " ")
}

// appleScriptString quotes s as an AppleScript string literal.
func appleScriptString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// osascriptArgv builds the elevation command. There are two quoting
// layers: each word is single-quoted for the shell that `do shell script`
// starts, and the whole command is an AppleScript string literal
// (backslash and double quote escaped). osascript itself is exec'd with an
// argv, so no third shell is involved. Control characters are refused:
// they cannot be represented safely in either layer.
func osascriptArgv(exe string, args []string) ([]string, error) {
	for _, a := range append([]string{exe}, args...) {
		if strings.ContainsFunc(a, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return nil, fmt.Errorf("refusing argument with control characters: %q", a)
		}
	}
	script := "do shell script " + appleScriptString(shellCommand(exe, args)) + " with administrator privileges"
	return []string{"/usr/bin/osascript", "-e", script}, nil
}

func defaultHelperBin() string {
	if exe, err := executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "duet-helper")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "./duet-helper"
}

// ---- health and adopt over RPC ----

func runHelperHealth(args []string) error {
	fs, cf := newFlagSet("helper health", "usage: duet helper health [--run] [--json]\n\n")
	var run bool
	fs.BoolVar(&run, "run", false, "run the checks now instead of showing the helper's last report")
	if _, done, err := parse(fs, args); done || err != nil {
		return err
	}
	method, params := rpc.MethodHealthLast, any(nil)
	if run {
		method, params = rpc.MethodHealthRun, rpc.HealthRunParams{Reason: "cli"}
	}
	var r rpc.HealthReport
	return cf.callPrint(method, params, &r, func() { printHealth(stdout, r) })
}
