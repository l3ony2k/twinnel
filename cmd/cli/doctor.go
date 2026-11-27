package main

// twinnel doctor: one body for every OS; newHealthEnv (doctor_<os>.go) builds
// the platform's health.Env.

import (
	"context"
	"errors"
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
	Secret     string
	ConfigPath string
	Settings   *health.Settings
}

// localHealthEnv is the Env of a local check run (doctor, MCP's doctor):
// this machine read in-process, pointed at the controller the helper
// reports (controllerFor). Without the helper the controller and its
// secret come from the mihomo config the platform discovers, or
// flagController.
func localHealthEnv(ctx context.Context, socket, flagController, configPath string, settings *health.Settings) (health.Env, error) {
	o := healthEnvOptions{Controller: flagController, ConfigPath: configPath, Settings: settings}
	if t := controllerFor(ctx, socket, flagController); t.Source != controllerFromApp {
		o.Controller, o.Secret = t.Controller, t.Secret
	}
	return newHealthEnv(o)
}

func init() {
	register("doctor", "run the mihomo + Tailscale coexistence health checks; --fix <check> fixes one", runDoctor)
}

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	markdown := fs.Bool("markdown", false, "print the report as Markdown")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	controller := fs.String("controller", "", "mihomo controller `HOST:PORT` (default: the helper's, else the config's, else 127.0.0.1:9090)")
	configPath := fs.String("mihomo-config", "", "mihomo config file (default: "+configDiscovery+")")
	groups := fs.String("sensitive-groups", "", "comma-separated proxy groups that must never go DIRECT (default: the leak guard's groups in the helper's settings, else "+strings.Join(health.DefaultSettings().SensitiveGroups, ",")+")")
	allowExit := fs.Bool("allow-exit-node", false, "accept a Tailscale exit node (warn instead of fail)")
	only := fs.String("only", "", "comma-separated check IDs to run")
	timeout := fs.Duration("timeout", 15*time.Second, "per-check timeout")
	strict := fs.Bool("strict", false, "exit with status 1 when any check fails")
	egress := fs.Bool("egress", false, "also run the egress test: one HTTPS GET to --egress-url directly and one through mihomo, compared (opt-in, sends no personal data; default: on when the helper's 定期检测 is)")
	egressURL := fs.String("egress-url", health.DefaultEgressURL, "neutral IP-echo `URL` for --egress (Cloudflare trace or a plain-IP body)")
	fix := fs.String("fix", "", "fix the failure reasons of check `ID` through the helper: preview each fix, ask, apply, check again")
	reason := fs.String("reason", "", "with --fix: only this failure reason")
	yes := fs.Bool("yes", false, "with --fix: apply without asking")
	undo := fs.String("undo", "", "put a fix back: the undo `JSON` --fix printed")
	lang := fs.String("lang", "", "language of --fix texts (zh-Hans or en)")
	socket := fs.String("socket", rpc.SocketFromEnv(), "helper socket `PATH`: the controller, the checks' settings, --fix/--undo (env TWINNEL_SOCKET)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	registry := health.DefaultRegistry()
	onlyIDs := splitList(*only)
	if err := registry.CheckIDs(onlyIDs); err != nil {
		return fmt.Errorf("--only: %w", err)
	}
	if *fix != "" {
		if err := registry.CheckIDs([]string{*fix}); err != nil {
			return fmt.Errorf("--fix: %w", err)
		}
	}
	if *fix != "" || *undo != "" {
		return runDoctorFix(fixOptions{check: *fix, reason: *reason, undo: *undo, yes: *yes, json: *asJSON,
			socket: *socket, lang: *lang, in: os.Stdin, out: os.Stdout})
	}
	if *markdown && *asJSON {
		return fmt.Errorf("--markdown and --json are exclusive")
	}

	// The helper's settings are the defaults; a flag given overrides.
	ctx := context.Background()
	settings, _ := healthSettings(ctx, *socket)
	if err := doctorOverrides(&settings, set, *groups, *egress); err != nil {
		return err
	}
	settings.AllowExitNode = *allowExit
	settings.EgressURL = *egressURL

	env, err := localHealthEnv(ctx, *socket, *controller, *configPath, &settings)
	if err != nil {
		return err
	}
	runner := &health.Runner{Registry: registry, Timeout: *timeout, Only: onlyIDs}
	rep := runner.Run(ctx, env)

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

// doctorOverrides applies the flags given (set) over the helper's
// settings: --sensitive-groups and --egress override, absent ones keep
// what the user chose in the app.
func doctorOverrides(s *health.Settings, set map[string]bool, groups string, egress bool) error {
	if set["sensitive-groups"] {
		if s.SensitiveGroups = splitList(groups); len(s.SensitiveGroups) == 0 {
			return errors.New("--sensitive-groups: no group named")
		}
	}
	if set["egress"] {
		s.Egress = egress
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
