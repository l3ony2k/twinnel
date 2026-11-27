package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// stubEnv is a local run's Env that knows only its platform and settings;
// every other read panics (the runner turns that into a failed check).
type stubEnv struct {
	health.Env
	settings health.Settings
}

func (stubEnv) Platform() string            { return "darwin" }
func (e stubEnv) Settings() health.Settings { return e.settings }
func (stubEnv) EgressSupported() bool       { return false }
func (stubEnv) MihomoRuntime(context.Context) (*health.MihomoRuntime, error) {
	return nil, errors.New("stub: no controller")
}

// fakeLocalEnv replaces the platform Env; it records the options of each
// local run (none: the helper ran the checks).
func fakeLocalEnv(t *testing.T) *[]healthEnvOptions {
	t.Helper()
	var runs []healthEnvOptions
	old := newHealthEnv
	newHealthEnv = func(o healthEnvOptions) (health.Env, error) {
		runs = append(runs, o)
		return stubEnv{settings: *o.Settings}, nil
	}
	t.Cleanup(func() { newHealthEnv = old })
	return &runs
}

// helperReport is a synthetic health.run answer.
func helperReport(t *testing.T, checks ...health.CheckReport) rpc.HealthReport {
	t.Helper()
	rep := health.Report{Platform: "windows", Overall: health.StatusOK, Checks: checks, Lang: "en"}
	for _, c := range checks {
		rep.Overall = health.Worse(rep.Overall, c.Status)
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	return rpc.HealthReport{Report: b, Reason: "cli", At: time.Unix(1, 0)}
}

func check(id string, st health.Status) health.CheckReport {
	return health.CheckReport{ID: id, Title: id, Result: health.Result{Status: st, Summary: id + " " + string(st)}}
}

// The helper runs the checks: its verdicts (here the controller owner and
// the DNS watchdog's, which a local run cannot know) are what doctor
// prints; --only filters them and works the overall status out again.
func TestDoctorRunsThroughHelper(t *testing.T) {
	clearControllerEnv(t)
	fakeAppConfig(t, "")
	local := fakeLocalEnv(t)
	asked := fakeHelperRPC(t, map[string]any{
		rpc.MethodHealthRun: helperReport(t, check("controller-owner", health.StatusFail), check("system-dns", health.StatusOK)),
	})
	rep, err := doctorReport(context.Background(), doctorRequest{socket: "/x.sock"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Source != health.SourceHelper || len(rep.Checks) != 2 || rep.Overall != health.StatusFail || len(*local) != 0 {
		t.Fatalf("report %+v, local runs %d", rep, len(*local))
	}
	if !slices.Equal(*asked, []string{rpc.MethodHealthRun}) {
		t.Errorf("asked %v", *asked)
	}
	if strings.Contains(rep.Text(), "Local run") {
		t.Error("the helper's report says it is local")
	}

	rep, err = doctorReport(context.Background(), doctorRequest{socket: "/x.sock", only: []string{"system-dns"}})
	if err != nil || len(rep.Checks) != 1 || rep.Checks[0].ID != "system-dns" || rep.Overall != health.StatusOK {
		t.Fatalf("--only: %+v %v", rep, err)
	}
}

// --egress asks the helper for the egress test unless its runs include it.
func TestDoctorEgressThroughHelper(t *testing.T) {
	fakeLocalEnv(t)
	egress := check(health.LeakEgress{}.ID(), health.StatusOK)
	for _, on := range []bool{false, true} {
		asked := fakeHelperRPC(t, map[string]any{
			rpc.MethodHealthRun:    helperReport(t, check("system-dns", health.StatusOK)),
			rpc.MethodSettingsGet:  rpc.Settings{HealthEgress: on},
			rpc.MethodHealthEgress: helperReport(t, check("system-dns", health.StatusOK), egress),
		})
		rep, err := doctorReport(context.Background(), doctorRequest{socket: "/x.sock", egress: true})
		if err != nil {
			t.Fatal(err)
		}
		ran := slices.Contains(*asked, rpc.MethodHealthEgress)
		if ran == on || (!on && len(rep.Checks) != 2) {
			t.Errorf("定期检测 %v: asked %v, %d checks", on, *asked, len(rep.Checks))
		}
	}
}

// No helper: the checks run here, and the report says so.
func TestDoctorFallsBackWithoutHelper(t *testing.T) {
	clearControllerEnv(t)
	fakeAppConfig(t, "")
	old := doctorBinaries
	doctorBinaries = func() []health.BinaryRef { return nil }
	t.Cleanup(func() { doctorBinaries = old })
	local := fakeLocalEnv(t)
	fakeHelperRPC(t, nil)
	var warned []string
	rep, err := doctorReport(context.Background(), doctorRequest{socket: "/x.sock", only: []string{"controller-owner"},
		timeout: time.Second, warn: func(s string) { warned = append(warned, s) }})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Source != health.SourceLocalNoHelper || len(*local) != 1 || len(warned) != 1 {
		t.Fatalf("source %q, local runs %d, warned %q", rep.Source, len(*local), warned)
	}
	if txt := (&health.Report{Lang: "en", Source: rep.Source}).Text(); !strings.Contains(txt, "helper did not answer") {
		t.Errorf("text: %s", txt)
	}
}

// --local (or a flag that changes the checks' inputs) runs here even with
// a helper, and the run still knows who holds the controller.
func TestDoctorLocalKnowsOwner(t *testing.T) {
	clearControllerEnv(t)
	fakeAppConfig(t, "")
	old := doctorBinaries
	doctorBinaries = func() []health.BinaryRef { return nil }
	t.Cleanup(func() { doctorBinaries = old })
	local := fakeLocalEnv(t)
	owner := &foreign.Owner{Addr: "127.0.0.1:9090", Verdict: foreign.Foreign, PID: 4321, Exe: `C:\other\mihomo.exe`}
	asked := fakeHelperRPC(t, map[string]any{
		rpc.MethodControllerCredentials: rpc.ControllerCredentials{Controller: "127.0.0.1:9090", Secret: "s"},
		rpc.MethodStatus:                rpc.Status{Mihomo: rpc.MihomoStatus{Controller: "127.0.0.1:9090", Owner: owner}},
		rpc.MethodSettingsGet:           rpc.Settings{},
	})
	rep, err := doctorReport(context.Background(), doctorRequest{socket: "/x.sock", local: true, only: []string{"controller-owner"}, timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(*asked, rpc.MethodHealthRun) || len(*local) != 1 || rep.Source != health.SourceLocal {
		t.Fatalf("asked %v, local runs %d, source %q", *asked, len(*local), rep.Source)
	}
	if rep.Controller == nil || !rep.Controller.IsForeign() || len(rep.Checks) != 1 || rep.Checks[0].Status != health.StatusFail {
		t.Fatalf("owner not used: %+v", rep)
	}
}

// Another program's mihomo on the controller: commands that would change
// it refuse, and say what holds it.
func TestControllerForForeignOwner(t *testing.T) {
	clearControllerEnv(t)
	fakeAppConfig(t, "")
	owner := &foreign.Owner{Addr: "127.0.0.1:9090", Verdict: foreign.Foreign, Client: foreign.ClientClashParty, PID: 4321}
	fakeHelperRPC(t, map[string]any{
		rpc.MethodControllerCredentials: rpc.ControllerCredentials{Controller: "127.0.0.1:9090"},
		rpc.MethodStatus:                rpc.Status{Mihomo: rpc.MihomoStatus{Owner: owner}},
	})
	t0 := controllerFor(context.Background(), "/x.sock", "")
	if err := t0.writable(); !errors.Is(err, errForeignController) || !strings.Contains(err.Error(), "Clash Party, pid 4321") {
		t.Fatalf("writable: %v", err)
	}
	// --controller points elsewhere: the helper's owner is not about it.
	if t1 := controllerFor(context.Background(), "/x.sock", "127.0.0.1:19090"); t1.Owner != nil || t1.writable() != nil || t1.Helper == nil {
		t.Fatalf("flag: %+v", t1)
	}
}
