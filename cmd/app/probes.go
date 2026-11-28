package main

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/health/fixture"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
	"github.com/l3ony2k/twinnel/core/version"
	"github.com/l3ony2k/twinnel/platform/common"
)

// machineProbes are every read the app makes of the computer it runs on
// (outside the helper and the mihomo controller, which are addressed by
// TWINNEL_SOCKET and TWINNEL_CONTROLLER): the health checks' facts, the live
// mihomo's launchd setup, Tailscale's LocalAPI and its install. A normal
// run uses hostProbes; a shot run uses shotProbes, which answer from
// fixtures only, so a screenshot never shows the owner's machine.
type machineProbes interface {
	// healthEnv is the Env the in-process checks read; nil on platforms
	// without checks.
	healthEnv(cfg status.Config, settings health.Settings) health.Env
	// liveSetup finds the mihomo that runs before Twinnel takes it over.
	liveSetup() liveSetup
	tailscaleAPI() tailscale.API
	tailscaleInstall() tailscale.Install
	// runCLI runs the Twinnel command line tool (version, the helper install
	// plan, the upgrade check, and the helper operations the user starts).
	runCLI(ctx context.Context, args ...string) (string, error)
	// processes lists the running processes (ps), for the overview's
	// memory, CPU and uptime of mihomo and tailscaled.
	processes(ctx context.Context) ([]status.ProcessEntry, error)
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
func (realProbes) liveSetup() liveSetup { return probeLiveSetup() }
func (realProbes) processes(ctx context.Context) ([]status.ProcessEntry, error) {
	if list, ok, err := systemProcesses(ctx); ok { // Windows: no ps (procs_windows.go)
		return list, err
	}
	out, err := exec.CommandContext(ctx, "ps", status.PSArgs...).Output()
	if err != nil {
		return nil, err
	}
	return status.ParsePS(string(out)), nil
}
func (realProbes) tailscaleAPI() tailscale.API { return tailscale.DefaultAPI() }
func (realProbes) tailscaleInstall() tailscale.Install {
	return tailscale.DetectInstall(runtime.GOOS, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	})
}

// shotProbes answers from fixtures: the health Env is a fixture.Machine
// (TWINNEL_HEALTH_FIXTURE, else fixture.Synthetic) whose tailnet is the
// status file's, Tailscale is the status file (TWINNEL_TAILSCALE_STATUS_FILE),
// and nothing is detected as a live setup (shot flows set a.live).
//
// Each platform gets its own fixture (goos): Windows paths, adapters and
// install kinds on a Windows run, so a reviewer never sees brew or utun on
// Windows.
type shotProbes struct {
	goos       string
	machine    fixture.Machine
	statusFile string
}

// newShotProbes builds the shot run's probes; shotGuard checked the
// variables they need.
func newShotProbes(getenv func(string) string) (*shotProbes, error) {
	p := &shotProbes{goos: runtime.GOOS, machine: shotMachine(runtime.GOOS), statusFile: getenv(tailscale.StatusFileEnv)}
	if f := getenv(envHealthFixture); f != "" {
		m, err := fixture.Load(f)
		if err != nil {
			return nil, err
		}
		p.machine = m
	}
	return p, nil
}

// shotMachine is fixture.Synthetic in goos's terms: on Windows the
// adapters are named as Windows names them (Tailscale's wintun adapter
// "Tailscale", mihomo's TUN "Meta", the uplink "Wi-Fi"), and this
// computer runs Windows. Elsewhere it is fixture.Synthetic unchanged.
func shotMachine(goos string) fixture.Machine {
	m := fixture.Synthetic()
	if goos != "windows" {
		return m
	}
	names := map[string]string{"utun9": "Tailscale", "utun5": "Meta", "en0": "Wi-Fi"}
	rename := func(s *string) {
		if n, ok := names[*s]; ok {
			*s = n
		}
	}
	rename(&m.TailnetIface)
	rename(&m.TunIface)
	routes := make(map[string]health.RouteLookup, len(m.Routes))
	for k, r := range m.Routes {
		rename(&r.Interface)
		routes[k] = r
	}
	m.Routes = routes
	m.Table = slices.Clone(m.Table)
	for i := range m.Table {
		rename(&m.Table[i].Interface)
	}
	m.Interfaces = slices.Clone(m.Interfaces)
	for i := range m.Interfaces {
		rename(&m.Interfaces[i].Name)
	}
	m.Services = slices.Clone(m.Services)
	for i := range m.Services {
		rename(&m.Services[i].Device)
	}
	if m.Runtime != nil {
		rt := *m.Runtime
		rename(&rt.Configs.TUN.Device)
		m.Runtime = &rt
	}
	if m.Config != nil {
		c := *m.Config
		rename(&c.TUN.Device)
		c.Path = `C:\ProgramData\Twinnel\mihomo\config.yaml`
		m.Config = &c
	}
	if m.Tailscale != nil {
		ts := *m.Tailscale
		ts.Self.OS = "windows"
		m.Tailscale = &ts
	}
	return m
}

// envHealthFixture names a fixture.Machine JSON file for a shot run's
// health checks (default: fixture.Synthetic).
const envHealthFixture = "TWINNEL_HEALTH_FIXTURE"

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

func (p *shotProbes) liveSetup() liveSetup { return liveSetup{} }

// processes is a synthetic process list: mihomo and tailscaled with
// plausible figures, nothing from this machine.
// Commands are what each system's lister reports: a path from ps on
// macOS and Linux, the image name on Windows (platform/windows/procs.go).
func (p *shotProbes) processes(context.Context) ([]status.ProcessEntry, error) {
	mihomo, tailscaled := "/usr/local/bin/mihomo", "/opt/homebrew/opt/tailscale/bin/tailscaled"
	switch p.goos {
	case "windows":
		mihomo, tailscaled = "mihomo.exe", "tailscaled.exe"
	case "linux":
		mihomo, tailscaled = "/usr/local/bin/mihomo", "/usr/sbin/tailscaled"
	}
	return []status.ProcessEntry{
		{PID: 812, RSS: 61 << 20, CPU: 1.8, Elapsed: 9*24*time.Hour + 20*time.Hour + 38*time.Minute, Command: mihomo},
		{PID: 405, RSS: 27 << 20, CPU: 0.3, Elapsed: 10*24*time.Hour + time.Hour, Command: tailscaled},
	}, nil
}
func (p *shotProbes) tailscaleAPI() tailscale.API { return tailscale.FileAPI(p.statusFile) }

// tailscaleInstall is what DetectInstall finds on a typical machine of
// p.goos: the official Windows client, a distribution's package on Linux,
// brew's tailscaled on macOS.
func (p *shotProbes) tailscaleInstall() tailscale.Install {
	present, ok := shotTailscaleFiles[p.goos]
	if !ok {
		return tailscale.Install{Kind: tailscale.InstallTailscaled, Source: "brew", Daemon: "/opt/homebrew/opt/tailscale/bin/tailscaled"}
	}
	return tailscale.DetectInstall(p.goos, func(f string) bool { return slices.Contains(present, f) })
}

// shotTailscaleFiles are the Tailscale files a shot run's machine has on
// Linux and Windows (macOS: brew's tailscaled, as the shots always had).
var shotTailscaleFiles = map[string][]string{
	"linux":   {"/usr/sbin/tailscaled", "/run/tailscale/tailscaled.sock"},
	"windows": {tailscale.WindowsDaemon},
}

// shotCLILink is the Twinnel CLI link a shot shows (the 设置 page's agent
// snippet and command line tool rows), where the installer puts it on
// goos.
func shotCLILink(goos string) string {
	if goos == "windows" {
		return `C:\Users\example\AppData\Local\Programs\Twinnel\bin\twinnel.exe`
	}
	return "/usr/local/bin/twinnel"
}

// runCLI answers the read-only CLI calls a page makes with synthetic
// output and refuses the rest: a shot run never runs Twinnel, whose plan and
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
	return "", errors.New("shot run: Twinnel " + cmd + " is not run")
}

// shotInstallPlan is the synthetic `twinnel helper install --dry-run --json`.
//
//go:embed testdata/shots-install-plan.json
var shotInstallPlan string

// probesFor picks the run's probes: fixtures in shot mode, else the host.
func probesFor(getenv func(string) string) (machineProbes, error) {
	if getenv("TWINNEL_SHOTS") == "" {
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

// newMihomoFile reads the mihomo config file through the probes' health
// Env (the live config, or the fixture in shots); nil on platforms
// without one.
func newMihomoFile(p machineProbes, cfg status.Config) func(context.Context) (*health.MihomoConfig, error) {
	if p.healthEnv(cfg, health.DefaultSettings()) == nil {
		return nil
	}
	return func(ctx context.Context) (*health.MihomoConfig, error) {
		return p.healthEnv(cfg, health.DefaultSettings()).MihomoConfig(ctx)
	}
}

// shotGOOS is the platform a shot run's fixtures are for: its probes'
// (a test renders Windows from any machine), else this one.
func (a *app) shotGOOS() string {
	if p, ok := a.probes.(*shotProbes); ok && p.goos != "" {
		return p.goos
	}
	return runtime.GOOS
}
