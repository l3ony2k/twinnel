package main

// `twinnel setup`: the "set up this Mac" flow (core/setup) for a terminal,
// and for Linux, which has no app.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/setup"
)

func init() {
	register("setup", "set up this machine from nothing: helper, mihomo, subscription, DNS, Tailscale (| status)", runSetup)
}

const setupUsage = `usage: twinnel setup [flags]          run (or continue) the setup
       twinnel setup status [--json]  show which steps are done, change nothing

Steps: helper, subscription, mihomo, config, apply, dns, tailscale, login,
health. Each one is skipped when it is already done, so running setup again
continues where it stopped.

The subscription URL is read from --subscription, else $TWINNEL_SUBSCRIPTION_URL,
else asked for on the terminal (it is a secret: prefer the variable or the
prompt to a flag, which shell history keeps).

flags:
`

// setupEnv builds the flow's connections; variables for tests.
var (
	setupConnect = func(socket string) func(ctx context.Context) (setup.Caller, error) {
		return func(ctx context.Context) (setup.Caller, error) {
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			c, err := rpc.Dial(ctx, nil, socket)
			if err != nil {
				return nil, err
			}
			return c, nil
		}
	}
	setupInstallHelper = func(ctx context.Context) error {
		return runHelperInstall([]string{"--yes"})
	}
	setupOpenURL = func(u string) error {
		fmt.Fprintf(stdout, "\nOpen this page to log in to Tailscale:\n  %s\n\n", u)
		if os.Getenv("SSH_CONNECTION") != "" {
			return nil
		}
		name, args := urlOpener(runtime.GOOS, u)
		if p, err := exec.LookPath(name); err == nil {
			return exec.Command(p, args...).Start()
		}
		return nil
	}
	// setupSessionLogin starts the Tailscale login from this process (the
	// signed-in user) where the helper does not (Windows).
	setupSessionLogin = setup.LocalSessionLogin
	// setupImport restores an export zip through the helper's
	// config.import; a variable for tests.
	setupImport      = setup.ImportConfig
	setupInteractive = func() bool {
		fi, err := os.Stdin.Stat()
		return err == nil && fi.Mode()&os.ModeCharDevice != 0
	}
)

// urlOpener is the command that opens u in the browser: open (macOS),
// rundll32's URL handler (Windows: no shell, so the URL is never parsed
// by cmd.exe), xdg-open elsewhere.
func urlOpener(goos, u string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{u}
	case "windows":
		return "rundll32.exe", []string{"url.dll,FileProtocolHandler", u}
	}
	return "xdg-open", []string{u}
}

type setupFlags struct {
	*clientFlags
	o        setup.Options
	importF  string
	yes      bool
	stepOnly string
}

func runSetup(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "status":
			return runSetupFlow(args[1:], true)
		}
	}
	return runSetupFlow(args, false)
}

func runSetupFlow(args []string, statusOnly bool) error {
	fs, cf := newFlagSet("setup", setupUsage)
	f := &setupFlags{clientFlags: cf}
	fs.StringVar(&f.o.SubscriptionURL, "subscription", "", "subscription `URL` (see above for a safer way)")
	fs.StringVar(&f.o.SubscriptionName, "name", "", "the subscription's `NAME`")
	fs.StringVar(&f.importF, "import", "", "restore an export `ZIP` (subscription and overrides) from another machine")
	fs.StringVar(&f.o.Proxy, "proxy", "", "download `PROXY` for GitHub and Tailscale: direct, subscription (a temporary mihomo with the subscription's nodes) or http://, socks5:// URL (default: through mihomo when it runs, else direct)")
	fs.StringVar(&f.o.KernelChannel, "channel", "", "mihomo `CHANNEL`: stable or alpha")
	fs.BoolVar(&f.o.NoDNS, "no-dns", false, "leave the system DNS alone")
	fs.BoolVar(&f.o.NoTailscale, "no-tailscale", false, "no Tailscale install or login")
	fs.BoolVar(&f.yes, "yes", false, "never ask (fail where input is needed)")
	fs.StringVar(&f.stepOnly, "step", "", "run only `STEP` again (a retry)")
	fs.DurationVar(&f.o.LoginTimeout, "login-timeout", 10*time.Minute, "how long to wait for the Tailscale login")
	if _, done, err := parse(fs, args); done || err != nil {
		return err
	}
	if f.o.SubscriptionURL == "" {
		f.o.SubscriptionURL = os.Getenv("TWINNEL_SUBSCRIPTION_URL")
	}
	if f.importF != "" {
		b, err := readImport(f.importF)
		if err != nil {
			return err
		}
		f.o.ImportZip = b
		// The zip holds node credentials and maybe subscription URLs.
		fmt.Fprintln(os.Stderr, "twinnel: the export holds secrets (node credentials, subscription URLs); delete it once the setup is done")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	flow := setup.New(setup.Env{
		Connect:       setupConnect(cf.socket),
		InstallHelper: setupInstallHelper,
		OpenURL:       setupOpenURL,
		ImportConfig:  setupImport,
		LinkTailscale: setupLinkTailscale,
		SessionLogin:  setupSessionLogin,
	}, f.o)
	flow.Detect(ctx)
	if statusOnly {
		return printSetup(flow, cf.json, nil)
	}
	if !cf.json {
		flow.OnChange = progressPrinter(flow)
	}
	var err error
	for {
		if f.stepOnly != "" {
			err = flow.RunStep(ctx, setup.Step(f.stepOnly))
			if err == nil {
				err = flow.Run(ctx)
			}
			f.stepOnly = ""
		} else {
			err = flow.Run(ctx)
		}
		var se *setup.StepError
		if err == nil || !errors.As(err, &se) || f.yes || cf.json || !setupInteractive() {
			break
		}
		// Interactive: ask for what the step needs, then go on.
		if !askForStep(flow, se) {
			break
		}
	}
	return printSetup(flow, cf.json, err)
}

// maxImportZip bounds an export zip read into memory.
const maxImportZip = 64 << 20

func readImport(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() > maxImportZip {
		return nil, fmt.Errorf("%s is too large for an export", path)
	}
	return os.ReadFile(path)
}

// askForStep asks the terminal for what a failed or waiting step needs
// and reports whether to go on.
func askForStep(flow *setup.Flow, se *setup.StepError) bool {
	in := bufio.NewReader(stdin)
	ask := func(q string) string {
		fmt.Fprint(stdout, q)
		l, _ := in.ReadString('\n')
		return strings.TrimSpace(l)
	}
	switch se.Code {
	case setup.CodeNeedsSubscription:
		u := ask("Subscription URL (or the path of an export zip): ")
		if u == "" {
			return false
		}
		if strings.HasSuffix(strings.ToLower(u), ".zip") {
			b, err := readImport(u)
			if err != nil {
				fmt.Fprintln(stdout, "  ", err)
				return true
			}
			flow.Opts.ImportZip = b
		} else {
			flow.Opts.SubscriptionURL = u
		}
		return true
	case setup.CodeDownload:
		fmt.Fprintln(stdout, "The download failed. Try it through:")
		fmt.Fprintln(stdout, "  1) the subscription's proxy (a temporary mihomo)")
		fmt.Fprintln(stdout, "  2) another proxy (http:// or socks5:// address)")
		fmt.Fprintln(stdout, "  3) directly again")
		switch ask("Choice [1-3, empty to stop]: ") {
		case "1":
			flow.Opts.Proxy = rpc.ProxySubscription
		case "2":
			flow.Opts.Proxy = ask("Proxy address: ")
		case "3":
			flow.Opts.Proxy = rpc.ProxyDirect
		default:
			return false
		}
		return true
	}
	return strings.HasPrefix(strings.ToLower(ask("Retry "+string(se.Step)+"? [y/N] ")), "y")
}

// progressPrinter prints each step when it starts, ends or waits.
func progressPrinter(flow *setup.Flow) func() {
	last := map[setup.Step]setup.State{}
	return func() {
		for i, s := range flow.Steps() {
			if last[s.ID] == s.State {
				continue
			}
			last[s.ID] = s.State
			switch s.State {
			case setup.Running:
				fmt.Fprintf(stdout, "[%d/%d] %s …\n", i+1, len(setup.Order), s.ID)
			case setup.Done:
				line := fmt.Sprintf("[%d/%d] %s ✓", i+1, len(setup.Order), s.ID)
				if s.Detail != "" {
					line += " (" + s.Detail + ")"
				}
				fmt.Fprintln(stdout, line)
				for _, w := range s.Warnings {
					fmt.Fprintln(stdout, "      note:", w.Text)
				}
			case setup.Failed:
				fmt.Fprintf(stdout, "[%d/%d] %s ✗ %s\n", i+1, len(setup.Order), s.ID, s.Error)
			case setup.Skipped:
				fmt.Fprintf(stdout, "[%d/%d] %s skipped\n", i+1, len(setup.Order), s.ID)
			}
		}
	}
}

func printSetup(flow *setup.Flow, asJSON bool, err error) error {
	if asJSON {
		out := struct {
			Complete bool              `json:"complete"`
			Steps    []setup.StepState `json:"steps"`
			Error    string            `json:"error,omitempty"`
		}{flow.Complete(), flow.Steps(), ""}
		if err != nil {
			out.Error = err.Error()
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return err
	}
	if err == nil && flow.Complete() {
		fmt.Fprintln(stdout, "Setup complete.")
		return nil
	}
	if err == nil { // status
		for _, s := range flow.Steps() {
			fmt.Fprintf(stdout, "  %-12s %s", s.ID, s.State)
			if s.Detail != "" {
				fmt.Fprintf(stdout, " (%s)", s.Detail)
			}
			fmt.Fprintln(stdout)
		}
	}
	return err
}
