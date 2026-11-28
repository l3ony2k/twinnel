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
	// health.last first: the fake has none, so the helper runs them.
	if !slices.Equal(*asked, []string{rpc.MethodHealthLast, rpc.MethodHealthRun}) {
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

// lastReport is a synthetic health.last answer: a run of 5 s that ended
// at end, worded in lang.
func lastReport(t *testing.T, end time.Time, lang string, checks ...health.CheckReport) rpc.HealthReport {
	t.Helper()
	rep := health.Report{Platform: "windows", Started: end.Add(-5 * time.Second), Duration: 5 * time.Second,
		Overall: health.StatusOK, Checks: checks, Lang: lang}
	for _, c := range checks {
		rep.Overall = health.Worse(rep.Overall, c.Status)
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	return rpc.HealthReport{Report: b, Reason: "timer", At: end}
}

// atGen sets a health.last answer's report and state generations.
func atGen(hr rpc.HealthReport, gen, state uint64) rpc.HealthReport {
	hr.Gen, hr.StateGen = gen, state
	return hr
}

// The helper's last report stands in for a run when it is recent and
// covers what was asked; the report says how old it is. Anything it does
// not cover, or --fresh, asks for a run.
func TestDoctorReusesRecentReport(t *testing.T) {
	fakeLocalEnv(t)
	end := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	egressOK := check(health.LeakEgress{}.ID(), health.StatusOK)
	egressOff := check(health.LeakEgress{}.ID(), health.StatusSkip)
	egressOld := egressOK
	egressOld.At = end.Add(-10 * time.Minute)
	dns := check("system-dns", health.StatusOK)
	for _, tc := range []struct {
		name  string
		age   time.Duration
		last  rpc.HealthReport
		q     doctorRequest
		reuse bool
	}{
		{"recent", 20 * time.Second, lastReport(t, end, "en", dns), doctorRequest{}, true},
		{"just now", 0, lastReport(t, end, "en", dns), doctorRequest{}, true},
		{"too old", 2 * time.Minute, lastReport(t, end, "en", dns), doctorRequest{}, false},
		{"--fresh", time.Second, lastReport(t, end, "en", dns), doctorRequest{fresh: true}, false},
		{"--lang same", time.Second, lastReport(t, end, "en", dns), doctorRequest{lang: "en-GB"}, true},
		{"--lang other", time.Second, lastReport(t, end, "en", dns), doctorRequest{lang: "zh"}, false},
		{"--lang default", time.Second, lastReport(t, end, "", dns), doctorRequest{lang: "zh-Hans"}, true},
		{"--only covered", time.Second, lastReport(t, end, "en", dns), doctorRequest{only: []string{"system-dns"}}, true},
		{"--only missing", time.Second, lastReport(t, end, "en", dns), doctorRequest{only: []string{"tun-dataplane"}}, false},
		{"--egress measured", time.Second, lastReport(t, end, "en", dns, egressOK), doctorRequest{egress: true}, true},
		{"--egress off", time.Second, lastReport(t, end, "en", dns, egressOff), doctorRequest{egress: true}, false},
		{"--egress carried, old", time.Second, lastReport(t, end, "en", dns, egressOld), doctorRequest{egress: true}, false},
		{"none yet", time.Second, rpc.HealthReport{}, doctorRequest{}, false},
		// The state generation: a report taken before the helper's latest
		// state change (mihomo stopped by someone else, the controller's
		// owner, the DNS watchdog) is not reused however young it is.
		{"current generation", time.Second, atGen(lastReport(t, end, "en", dns), 7, 7), doctorRequest{}, true},
		{"older generation", time.Second, atGen(lastReport(t, end, "en", dns), 6, 7), doctorRequest{}, false},
		{"older generation, --only", time.Second, atGen(lastReport(t, end, "en", dns), 6, 7), doctorRequest{only: []string{"system-dns"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asked := fakeHelperRPC(t, map[string]any{
				rpc.MethodHealthLast:   tc.last,
				rpc.MethodHealthRun:    helperReport(t, dns, egressOK),
				rpc.MethodSettingsGet:  rpc.Settings{HealthEgress: true},
				rpc.MethodHealthEgress: helperReport(t, dns, egressOK),
			})
			q := tc.q
			q.socket = "/x.sock"
			q.now = func() time.Time { return end.Add(tc.age) }
			rep, err := doctorReport(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			ran := slices.Contains(*asked, rpc.MethodHealthRun)
			if ran == tc.reuse || rep.Source != health.SourceHelper {
				t.Fatalf("reuse %v: asked %v, source %q", tc.reuse, *asked, rep.Source)
			}
			if tc.q.fresh && slices.Contains(*asked, rpc.MethodHealthLast) {
				t.Errorf("--fresh asked for the last report: %v", *asked)
			}
			if !tc.reuse {
				if rep.Age != 0 || strings.Contains(rep.Text(), "last run") {
					t.Errorf("a run now has an age: %v", rep.Age)
				}
				return
			}
			want := max(time.Second, tc.age)
			if rep.Age != want {
				t.Errorf("age %v, want %v", rep.Age, want)
			}
			b, _ := rep.JSON()
			if !strings.Contains(string(b), `"ageNs"`) {
				t.Errorf("JSON has no age: %s", b)
			}
		})
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
