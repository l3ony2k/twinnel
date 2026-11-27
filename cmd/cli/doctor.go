package main

// twinnel doctor: the helper's health run, the one the app shows. A local
// run (newHealthEnv, doctor_<os>.go: the platform's health.Env) only with
// --local, a flag that changes the checks' inputs, or without a helper.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/rpc/healthsrc"
)

// healthEnvOptions are what a local run's platform Env is pointed at.
type healthEnvOptions struct {
	Controller string
	Secret     string
	ConfigPath string
	Settings   *health.Settings
}

// newHealthEnv builds a local run's Env (platformHealthEnv,
// doctor_<os>.go); replaced in tests.
var newHealthEnv = platformHealthEnv

// doctorRequest is one doctor run (twinnel doctor, MCP's doctor tool).
// The helper runs the checks: its report is the one the app shows, with
// what only it knows (who holds the controller, the competitors'
// autostarts, the DNS it guards). This process runs them itself only when
// asked (local) or when the helper does not answer, and the report says
// so (Report.Source).
type doctorRequest struct {
	socket string
	// local runs the checks here (--local, or a flag that changes their
	// inputs: the helper's run has its own).
	local bool
	only  []string
	// egress also runs the egress test now when the helper's 定期检测
	// does not (--egress).
	egress bool
	// lang words the report; "" keeps the helper's language (a local run:
	// health.DefaultLang).
	lang string
	// Local runs only: the per-check timeout, the controller and config
	// file, and changes to the helper's settings (the flags given).
	timeout    time.Duration
	controller string
	configPath string
	adjust     func(*health.Settings) error
	// warn gets a line about falling back to a local run (stderr).
	warn func(string)
}

// doctorReport runs the checks as q asks.
func doctorReport(ctx context.Context, q doctorRequest) (*health.Report, error) {
	if !q.local {
		rep, err := helperHealth(ctx, q)
		if err == nil {
			rep.Source = health.SourceHelper
			return rep.Only(q.only), nil
		}
		if q.warn != nil {
			q.warn(fmt.Sprintf("the helper did not run the checks (%v); running them here", err))
		}
	}
	rep, err := localHealth(ctx, q)
	if err != nil {
		return nil, err
	}
	rep.Source = health.SourceLocal
	if !q.local {
		rep.Source = health.SourceLocalNoHelper
	}
	return rep, nil
}

// helperCaller calls the helper at a socket, one connection per call.
type helperCaller string

func (s helperCaller) Call(ctx context.Context, method string, params, result any) error {
	return helperRPC(ctx, string(s), method, params, result)
}

// helperHealth asks the helper to run every check now (health.run) and,
// with q.egress, the egress test too (health.egress) unless its runs
// include it already.
func helperHealth(ctx context.Context, q doctorRequest) (*health.Report, error) {
	src := healthsrc.Source{Client: helperCaller(q.socket), Reason: "cli", Lang: q.lang}
	rep, err := src.RunNow(ctx)
	if err != nil || !q.egress {
		return rep, err
	}
	var set rpc.Settings
	if err := helperAsk(ctx, q.socket, rpc.MethodSettingsGet, nil, &set); err == nil && set.HealthEgress {
		return rep, nil
	}
	var hr rpc.HealthReport
	if err := helperRPC(ctx, q.socket, rpc.MethodHealthEgress, rpc.HealthEgressParams{Lang: q.lang}, &hr); err != nil {
		return nil, fmt.Errorf("egress test: %w", err)
	}
	if rep, err = healthsrc.Decode(hr); err == nil && rep == nil {
		err = healthsrc.ErrNoReport
	}
	return rep, err
}

// localHealth runs the checks in this process: the helper's settings
// (the defaults without it) with q's changes, the controller the helper
// reports (controllerFor) and, when the helper said, who holds it.
func localHealth(ctx context.Context, q doctorRequest) (*health.Report, error) {
	settings, _ := healthSettings(ctx, q.socket)
	if q.adjust != nil {
		if err := q.adjust(&settings); err != nil {
			return nil, err
		}
	}
	o := healthEnvOptions{Controller: q.controller, ConfigPath: q.configPath, Settings: &settings}
	t := controllerFor(ctx, q.socket, q.controller)
	if t.Source != controllerFromApp {
		o.Controller, o.Secret = t.Controller, t.Secret
	}
	env, err := newHealthEnv(o)
	if err != nil {
		return nil, err
	}
	if t.Owner != nil {
		owner := *t.Owner
		env = health.WithOwner(env, func(context.Context) foreign.Owner { return owner })
	}
	if q.lang != "" {
		ctx = health.WithLang(ctx, q.lang)
	}
	runner := &health.Runner{Registry: health.DefaultRegistry(), Timeout: q.timeout, Only: q.only}
	return runner.Run(ctx, env), nil
}

// localOnlyFlags change the checks' inputs: given, the checks run here.
var localOnlyFlags = []string{"controller", "mihomo-config", "sensitive-groups", "allow-exit-node", "egress-url"}

func init() {
	register("doctor", "run the mihomo + Tailscale coexistence health checks; --fix <check> fixes one", runDoctor)
}

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	markdown := fs.Bool("markdown", false, "print the report as Markdown")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	local := fs.Bool("local", false, "run the checks in this process instead of the helper (also when "+strings.Join(prefixed(localOnlyFlags), ", ")+" or --egress=false is given, or no helper answers)")
	controller := fs.String("controller", "", "local run: mihomo controller `HOST:PORT` (default: the helper's, else the config's, else 127.0.0.1:9090)")
	configPath := fs.String("mihomo-config", "", "local run: mihomo config file (default: "+configDiscovery+")")
	groups := fs.String("sensitive-groups", "", "local run: comma-separated proxy groups that must never go DIRECT (default: the leak guard's groups in the helper's settings, else "+strings.Join(health.DefaultSettings().SensitiveGroups, ",")+")")
	allowExit := fs.Bool("allow-exit-node", false, "local run: accept a Tailscale exit node (warn instead of fail)")
	only := fs.String("only", "", "comma-separated check IDs to show (a local run runs only those)")
	timeout := fs.Duration("timeout", 15*time.Second, "per-check timeout of a local run")
	strict := fs.Bool("strict", false, "exit with status 1 when any check fails")
	egress := fs.Bool("egress", false, "also run the egress test now: one HTTPS GET to an IP-echo URL directly and one through mihomo, compared (opt-in, sends no personal data; default: on when the helper's 定期检测 is)")
	var egressURLs []string
	fs.Func("egress-url", "local run: IP-echo `URL`s for --egress, tried in order until one answers (comma-separated or repeated; default: the helper's list, else "+strings.Join(health.DefaultEgressURLs, ",")+")", func(s string) error {
		egressURLs = append(egressURLs, splitList(s)...)
		return nil
	})
	fix := fs.String("fix", "", "fix the failure reasons of check `ID` through the helper: preview each fix, ask, apply, check again")
	reason := fs.String("reason", "", "with --fix: only this failure reason")
	yes := fs.Bool("yes", false, "with --fix: apply without asking")
	undo := fs.String("undo", "", "put a fix back: the undo `JSON` --fix printed")
	lang := fs.String("lang", "", "language of the report and of --fix texts (zh-Hans or en; default: the helper's)")
	socket := fs.String("socket", rpc.SocketFromEnv(), "helper socket `PATH`: the helper runs the checks and --fix/--undo (env TWINNEL_SOCKET)")
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
	if err := health.CheckEgressURLs(egressURLs); err != nil {
		return err
	}

	q := doctorRequest{socket: *socket, local: *local, only: onlyIDs, egress: *egress, lang: *lang,
		timeout: *timeout, controller: *controller, configPath: *configPath,
		warn: func(s string) { fmt.Fprintln(os.Stderr, "twinnel: "+s) }}
	for _, f := range localOnlyFlags {
		q.local = q.local || set[f]
	}
	q.local = q.local || (set["egress"] && !*egress)
	// The helper's settings are the defaults; a flag given overrides.
	q.adjust = func(s *health.Settings) error {
		if err := doctorOverrides(s, set, *groups, *egress); err != nil {
			return err
		}
		s.AllowExitNode = *allowExit
		if len(egressURLs) > 0 {
			s.EgressURLs = egressURLs
		}
		return nil
	}
	rep, err := doctorReport(context.Background(), q)
	if err != nil {
		return err
	}

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

func prefixed(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "--" + n
	}
	return out
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
