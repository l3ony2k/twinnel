package main

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/health/fixture"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/tailscale"
	"github.com/l3ony2k/duet/core/version"
	"github.com/l3ony2k/duet/platform/common"
)

// machineProbes are every read the app makes of the computer it runs on
// (outside the helper and the mihomo controller, which are addressed by
// DUET_SOCKET and DUET_CONTROLLER): the health checks' facts, the live
// mihomo's launchd setup, Tailscale's LocalAPI and its install. A normal
// run uses hostProbes; a shot run uses shotProbes, which answer from
// fixtures only, so a screenshot never shows the owner's machine.
type machineProbes interface {
	// healthEnv is the Env the in-process checks read; nil on platforms
	// without checks.
	healthEnv(cfg status.Config, settings health.Settings) health.Env
	// liveSetup finds the mihomo that runs before duet takes it over.
	liveSetup() liveSetup
	tailscaleAPI() tailscale.API
	tailscaleInstall() tailscale.Install
	// runCLI runs the duet command line tool (version, the helper install
	// plan, the upgrade check, and the helper operations the user starts).
	runCLI(ctx context.Context, args ...string) (string, error)
}

// hostProbes reads the real machine. Tests replace it with a probe set
// that fails on any call, to prove shot runs never reach it.
var hostProbes machineProbes = realProbes{}

// realProbes reads this computer (health_darwin.go / health_other.go for
// the health Env).
type realProbes struct{}

func (realProbes) runCLI(ctx context.Context, args ...string) (string, error) {
	return execCLI(ctx, args...)
}
func (realProbes) liveSetup() liveSetup        { return probeLiveSetup() }
func (realProbes) tailscaleAPI() tailscale.API { return tailscale.DefaultAPI() }
func (realProbes) tailscaleInstall() tailscale.Install {
	return tailscale.DetectInstall(runtime.GOOS, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	})
}

// shotProbes answers from fixtures: the health Env is a fixture.Machine
// (DUET_HEALTH_FIXTURE, else fixture.Synthetic) whose tailnet is the
// status file's, Tailscale is the status file (DUET_TAILSCALE_STATUS_FILE),
// and nothing is detected as a live setup (shot flows set a.live).
type shotProbes struct {
	machine    fixture.Machine
	statusFile string
}

// newShotProbes builds the shot run's probes; shotGuard checked the
// variables they need.
func newShotProbes(getenv func(string) string) (*shotProbes, error) {
	p := &shotProbes{machine: fixture.Synthetic(), statusFile: getenv(tailscale.StatusFileEnv)}
	if f := getenv(envHealthFixture); f != "" {
		m, err := fixture.Load(f)
		if err != nil {
			return nil, err
		}
		p.machine = m
	}
	return p, nil
}

// envHealthFixture names a fixture.Machine JSON file for a shot run's
// health checks (default: fixture.Synthetic).
const envHealthFixture = "DUET_HEALTH_FIXTURE"

func (p *shotProbes) healthEnv(_ status.Config, settings health.Settings) health.Env {
	f := p.statusFile
	return &fixture.Env{M: p.machine, Prefs: settings, TailscaleFn: func(context.Context) (*health.TailscaleStatus, error) {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		return common.ParseTailscaleStatus(b)
	}}
}

func (p *shotProbes) liveSetup() liveSetup        { return liveSetup{} }
func (p *shotProbes) tailscaleAPI() tailscale.API { return tailscale.FileAPI(p.statusFile) }
func (p *shotProbes) tailscaleInstall() tailscale.Install {
	return tailscale.Install{Kind: tailscale.InstallTailscaled, Source: "brew", Daemon: "/opt/homebrew/opt/tailscale/bin/tailscaled"}
}

// runCLI answers the read-only CLI calls a page makes with synthetic
// output and refuses the rest: a shot run never runs duet, whose plan and
// checks read this machine and the installed helper.
func (p *shotProbes) runCLI(_ context.Context, args ...string) (string, error) {
	cmd := strings.Join(args, " ")
	switch {
	case cmd == "version --short":
		return version.String() + "\n", nil
	case strings.HasPrefix(cmd, "helper install ") && slices.Contains(args, "--dry-run"):
		return shotInstallPlan, nil
	case strings.HasPrefix(cmd, "helper upgrade ") && slices.Contains(args, "--check"):
		return `{"action":"none"}`, nil
	}
	return "", errors.New("shot run: duet " + cmd + " is not run")
}

// shotInstallPlan is the synthetic `duet helper install --dry-run`.
//
//go:embed testdata/shots-install-plan.txt
var shotInstallPlan string

// probesFor picks the run's probes: fixtures in shot mode, else the host.
func probesFor(getenv func(string) string) (machineProbes, error) {
	if getenv("DUET_SHOTS") == "" {
		return hostProbes, nil
	}
	return newShotProbes(getenv)
}

// newHealthSource runs the health checks in-process over the probes' Env,
// read-only; nil when the platform has none. The helper serves the same
// report over RPC when it is connected (healthsource.go).
func newHealthSource(p machineProbes, cfg status.Config) status.HealthSource {
	if p.healthEnv(cfg, health.DefaultSettings()) == nil {
		return nil
	}
	return status.RunnerSource{
		Runner: &health.Runner{Registry: health.DefaultRegistry(), Timeout: 15 * time.Second},
		Env:    func() health.Env { return p.healthEnv(cfg, health.DefaultSettings()) },
	}
}

// newEgressProbe runs the opt-in egress test once (health.LeakEgress with
// Settings.Egress on): one HTTPS request to an IP-echo address directly
// and one through mihomo. The 检测出口 button on the health page.
func newEgressProbe(p machineProbes, cfg status.Config) func(context.Context) *health.CheckReport {
	settings := health.DefaultSettings()
	settings.Egress = true
	if p.healthEnv(cfg, settings) == nil {
		return nil
	}
	return func(ctx context.Context) *health.CheckReport {
		r := &health.Runner{Registry: health.DefaultRegistry(), Timeout: 20 * time.Second, Only: []string{health.LeakEgress{}.ID()}}
		rep := r.Run(health.WithLang(ctx, i18n.Tag()), p.healthEnv(cfg, settings))
		if len(rep.Checks) == 0 {
			return nil
		}
		return &rep.Checks[0]
	}
}
