package main

// duet doctor: one body for every OS; newHealthEnv (doctor_<os>.go) builds
// the platform's health.Env.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/health"
)

// healthEnvOptions are the doctor flags that reach the platform Env.
type healthEnvOptions struct {
	Controller string
	ConfigPath string
	Settings   *health.Settings
}

func init() {
	register("doctor", "run the mihomo + Tailscale coexistence health checks (read-only)", runDoctor)
}

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	markdown := fs.Bool("markdown", false, "print the report as Markdown")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	controller := fs.String("controller", "", "mihomo controller host:port (default: from the config, else 127.0.0.1:9090)")
	configPath := fs.String("mihomo-config", "", "mihomo config file (default: "+configDiscovery+")")
	peers := fs.String("peers", "", "comma-separated Tailscale peers expected to connect directly (default: all online peers)")
	groups := fs.String("sensitive-groups", "", "comma-separated proxy groups that must never go DIRECT (default: Google,Meta)")
	allowExit := fs.Bool("allow-exit-node", false, "accept a Tailscale exit node (warn instead of fail)")
	only := fs.String("only", "", "comma-separated check IDs to run")
	timeout := fs.Duration("timeout", 15*time.Second, "per-check timeout")
	strict := fs.Bool("strict", false, "exit with status 1 when any check fails")
	egress := fs.Bool("egress", false, "also run the egress test: one HTTPS GET to --egress-url directly and one through mihomo, compared (opt-in, sends no personal data)")
	egressURL := fs.String("egress-url", health.DefaultEgressURL, "neutral IP-echo `URL` for --egress (Cloudflare trace or a plain-IP body)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *markdown && *asJSON {
		return fmt.Errorf("--markdown and --json are exclusive")
	}

	settings := health.DefaultSettings()
	if *peers != "" {
		settings.PreferredPeers = splitList(*peers)
	}
	if *groups != "" {
		settings.SensitiveGroups = splitList(*groups)
	}
	settings.AllowExitNode = *allowExit
	settings.Egress, settings.EgressURL = *egress, *egressURL

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
