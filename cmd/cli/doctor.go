package main

// twinnel doctor: one body for every OS; newHealthEnv (doctor_<os>.go) builds
// the platform's health.Env.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// healthEnvOptions are the doctor flags that reach the platform Env.
type healthEnvOptions struct {
	Controller string
	ConfigPath string
	Settings   *health.Settings
}

func init() {
	register("doctor", "run the mihomo + Tailscale coexistence health checks; --fix <check> fixes one", runDoctor)
}

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	markdown := fs.Bool("markdown", false, "print the report as Markdown")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	controller := fs.String("controller", "", "mihomo controller host:port (default: from the config, else 127.0.0.1:9090)")
	configPath := fs.String("mihomo-config", "", "mihomo config file (default: "+configDiscovery+")")
	groups := fs.String("sensitive-groups", "", "comma-separated proxy groups that must never go DIRECT (default: Google,Meta)")
	allowExit := fs.Bool("allow-exit-node", false, "accept a Tailscale exit node (warn instead of fail)")
	only := fs.String("only", "", "comma-separated check IDs to run")
	timeout := fs.Duration("timeout", 15*time.Second, "per-check timeout")
	strict := fs.Bool("strict", false, "exit with status 1 when any check fails")
	egress := fs.Bool("egress", false, "also run the egress test: one HTTPS GET to an IP-echo URL directly and one through mihomo, compared (opt-in, sends no personal data)")
	var egressURLs []string
	fs.Func("egress-url", "IP-echo `URL`s for --egress, tried in order until one answers (comma-separated or repeated; default "+strings.Join(health.DefaultEgressURLs, ",")+")", func(s string) error {
		egressURLs = append(egressURLs, splitList(s)...)
		return nil
	})
	fix := fs.String("fix", "", "fix the failure reasons of check `ID` through the helper: preview each fix, ask, apply, check again")
	reason := fs.String("reason", "", "with --fix: only this failure reason")
	yes := fs.Bool("yes", false, "with --fix: apply without asking")
	undo := fs.String("undo", "", "put a fix back: the undo `JSON` --fix printed")
	lang := fs.String("lang", "", "language of --fix texts (zh-Hans or en)")
	socket := fs.String("socket", rpc.SocketFromEnv(), "helper socket `PATH` for --fix/--undo (env TWINNEL_SOCKET)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fix != "" || *undo != "" {
		return runDoctorFix(fixOptions{check: *fix, reason: *reason, undo: *undo, yes: *yes, json: *asJSON,
			socket: *socket, lang: *lang, in: os.Stdin, out: os.Stdout})
	}
	if *markdown && *asJSON {
		return fmt.Errorf("--markdown and --json are exclusive")
	}

	settings := health.DefaultSettings()
	if *groups != "" {
		settings.SensitiveGroups = splitList(*groups)
	}
	settings.AllowExitNode = *allowExit
	if err := health.CheckEgressURLs(egressURLs); err != nil {
		return err
	}
	settings.Egress, settings.EgressURLs = *egress, egressURLs
	settings.Binaries = doctorBinaries()

	env, err := newHealthEnv(healthEnvOptions{Controller: *controller, ConfigPath: *configPath, Settings: &settings})
	if err != nil {
		return err
	}
	runner := &health.Runner{Registry: health.DefaultRegistry(), Timeout: *timeout, Only: splitList(*only)}
	rep := runner.Run(context.Background(), env)

	switch {
	case *asJSON:
		b, err := rep.JSON()
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	case *markdown:
		fmt.Print(rep.Markdown())
	default:
		fmt.Print(rep.Text())
	}
	if *strict && rep.Overall == health.StatusFail {
		os.Exit(1)
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
