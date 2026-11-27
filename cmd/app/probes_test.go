package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/tailscale"
)

// forbiddenProbes stands in for the host while a shot run is tested: any
// call is a read of the real machine and fails the test.
type forbiddenProbes struct {
	mu    sync.Mutex
	calls []string
}

func (f *forbiddenProbes) hit(what string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, what)
}

func (f *forbiddenProbes) healthEnv(status.Config, health.Settings) health.Env {
	f.hit("healthEnv")
	return nil
}
func (f *forbiddenProbes) runCLI(_ context.Context, args ...string) (string, error) {
	f.hit("runCLI " + strings.Join(args, " "))
	return "", nil
}
func (f *forbiddenProbes) liveSetup() liveSetup { f.hit("liveSetup"); return liveSetup{} }
func (f *forbiddenProbes) processes(context.Context) ([]status.ProcessEntry, error) {
	f.hit("processes")
	return nil, nil
}
func (f *forbiddenProbes) tailscaleAPI() tailscale.API {
	f.hit("tailscaleAPI")
	return tailscale.FileAPI("/nonexistent")
}
func (f *forbiddenProbes) tailscaleInstall() tailscale.Install {
	f.hit("tailscaleInstall")
	return tailscale.Install{}
}

// useForbiddenHost swaps hostProbes for a forbidden set for the test.
func useForbiddenHost(t *testing.T) *forbiddenProbes {
	t.Helper()
	f := &forbiddenProbes{}
	old := hostProbes
	hostProbes = f
	t.Cleanup(func() { hostProbes = old })
	return f
}

// Each shot flow's setup reads fixtures only: building the app, its
// Tailscale client and install, the in-process health checks, the egress
// test and the live-setup detection never touch the host probes.
func TestShotFlowsAreHermetic(t *testing.T) {
	host := useForbiddenHost(t)
	flows := []string{""} // the default pass
	for name := range (&app{}).shotFlows() {
		flows = append(flows, name)
	}
	sort.Strings(flows)
	for _, flow := range flows {
		t.Run("flow="+flow, func(t *testing.T) {
			env := scratchShotEnv(t)
			env["DUET_SHOTS_FLOW"] = flow
			t.Setenv("HOME", env["HOME"]) // os.UserConfigDir
			a, err := newApp(envFunc(env))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := a.probes.(*shotProbes); !ok {
				t.Fatalf("shot run uses %T, want *shotProbes", a.probes)
			}
			if a.cfg.AllowWrites {
				t.Error("shot run allows writes without DUET_READ_ONLY=0")
			}
			ctx := context.Background()
			cc := controllerConfig(a.cfg, a.creds.cur)
			if cc.Controller != env["DUET_CONTROLLER"] {
				t.Errorf("controller %q, want the fake %q", cc.Controller, env["DUET_CONTROLLER"])
			}
			src := newHealthSource(a.probes, cc)
			if src == nil {
				t.Fatal("no health source")
			}
			rep, err := src.Run(ctx)
			if err != nil || rep == nil || len(rep.Checks) == 0 {
				t.Fatalf("health run: %v %v", rep, err)
			}
			if e := newEgressProbe(a.probes, cc)(ctx); e == nil || e.Status != health.StatusOK {
				t.Errorf("egress probe: %+v", e)
			}
			if _, err := a.ts.State(ctx); err != nil {
				t.Errorf("tailscale from the status file: %v", err)
			}
			for _, args := range [][]string{{"version", "--short"}, installArgs("--dry-run"), upgradeArgs("--check", "--json")} {
				if out, err := a.runCLI(ctx, args...); err != nil || out == "" {
					t.Errorf("duet %v: %q %v", args, out, err)
				}
			}
			for _, args := range [][]string{installArgs("--yes"), upgradeArgs("--yes"), {"helper", "restart"}} {
				if _, err := a.runCLI(ctx, args...); err == nil {
					t.Errorf("shot run ran duet %v", args)
				}
			}
			if l := a.probes.liveSetup(); l.found() {
				t.Errorf("shot run detected a live setup: %+v", l)
			}
		})
	}
	if len(host.calls) > 0 {
		t.Fatalf("shot runs read the real machine: %v", host.calls)
	}
}

// The shot machine's tailnet is the status file's: health and pages agree.
func TestShotHealthUsesStatusFile(t *testing.T) {
	useForbiddenHost(t)
	p, err := newShotProbes(envFunc(scratchShotEnv(t)))
	if err != nil {
		t.Fatal(err)
	}
	ts, err := p.healthEnv(status.Config{}, health.DefaultSettings()).Tailscale(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ts.MagicDNSSuffix != "example-tailnet.ts.net" || len(ts.Peers) == 0 {
		t.Fatalf("tailnet %q with %d peers, want the status file's", ts.MagicDNSSuffix, len(ts.Peers))
	}
}

// A normal run reads the host.
func TestNormalRunUsesHost(t *testing.T) {
	host := useForbiddenHost(t)
	p, err := probesFor(envFunc(map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	if p != machineProbes(host) {
		t.Fatalf("normal run uses %T", p)
	}
}

// Shot code reaches the machine only through a.probes: no shot file calls
// a platform probe directly.
func TestShotFilesUseNoHostProbes(t *testing.T) {
	bad := regexp.MustCompile(`probeLiveSetup\(|takeoverprobe\.|darwin\.New\(|tailscale\.DefaultAPI\(|tailscale\.LocalAPI\(|tailscale\.DetectInstall\(|exec\.Command|realProbes\{|hostProbes|os\.UserHomeDir\(|execCLI\(`)
	files, _ := filepath.Glob("shots*.go")
	if len(files) == 0 {
		t.Fatal("no shot files")
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if bad.MatchString(line) {
				t.Errorf("%s:%d reads the host: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
