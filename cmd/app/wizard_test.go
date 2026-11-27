package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/setup"
	"github.com/l3ony2k/twinnel/core/status"
)

// The wizard's state survives a quit: it resumes at the step it was on.
func TestWizardStateRoundTrip(t *testing.T) {
	p := wizardStatePath(filepath.Join(t.TempDir(), "twinnel", "app.json"))
	if st := loadWizardState(p); st != (wizardState{}) {
		t.Fatalf("missing file: %+v", st)
	}
	want := wizardState{Step: wizSub, Later: true, TSSkipped: true}
	if err := saveWizardState(p, want); err != nil {
		t.Fatal(err)
	}
	if got := loadWizardState(p); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// A broken file is a fresh start; a step out of range is clamped.
	if err := os.WriteFile(p, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := loadWizardState(p); st != (wizardState{}) {
		t.Fatalf("broken file: %+v", st)
	}
	if err := os.WriteFile(p, []byte(`{"step": 42}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := loadWizardState(p); st.Step != wizDone {
		t.Fatalf("step 42 → %d, want %d", st.Step, wizDone)
	}
}

func snapHelper(state status.HelperState, st *rpc.Status) status.Snapshot {
	return status.Snapshot{Helper: status.Helper{State: state, Status: st}}
}

// What counts as "not set up yet".
func TestNeedsSetup(t *testing.T) {
	applied := &rpc.Status{ActiveSubscription: "main"}
	cases := []struct {
		name        string
		s           status.Snapshot
		need, known bool
	}{
		{"no helper", snapHelper(status.HelperNotInstalled, nil), true, true},
		{"helper, nothing applied", snapHelper(status.HelperConnected, &rpc.Status{}), true, true},
		{"helper, set up", snapHelper(status.HelperConnected, applied), false, true},
		{"helper, status not read", snapHelper(status.HelperConnected, nil), false, false},
		{"unknown yet", snapHelper(status.HelperUnknown, nil), false, false},
		{"installed but down", snapHelper(status.HelperNotRunning, nil), false, true},
	}
	for _, c := range cases {
		need, known := needsSetup(c.s)
		if need != c.need || known != c.known {
			t.Errorf("%s: need %v known %v, want %v %v", c.name, need, known, c.need, c.known)
		}
	}
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// The first launch of a machine not set up opens the wizard; finished,
// put off, set up, or a development run against a scratch helper does
// not. TWINNEL_WIZARD overrides; TWINNEL_OPEN=1 asks for windows (a real
// launch's), TWINNEL_OPEN=page:<id> for that page only.
func TestLaunchOpensWizard(t *testing.T) {
	fresh := snapHelper(status.HelperNotInstalled, nil)
	set := snapHelper(status.HelperConnected, &rpc.Status{ActiveSubscription: "main"})
	none := env(nil)
	// Windows QA round 1: a missing scratch pipe, no TWINNEL_OPEN.
	scratch := env(map[string]string{"TWINNEL_SOCKET": `\\.\pipe\twinnel-qa-none`})
	main := launchOpen{main: true}
	cases := []struct {
		name          string
		st            wizardState
		s             status.Snapshot
		getenv        func(string) string
		req           launchOpen
		open, decided bool
	}{
		{"fresh machine", wizardState{}, fresh, none, launchOpen{}, true, true},
		{"fresh machine, window asked for", wizardState{}, fresh, none, main, true, true},
		{"resumes mid-way", wizardState{Step: wizSub}, fresh, none, launchOpen{}, true, true},
		{"put off", wizardState{Later: true}, fresh, none, launchOpen{}, false, true},
		{"finished", wizardState{Done: true}, fresh, none, launchOpen{}, false, true},
		{"already set up", wizardState{}, set, none, launchOpen{}, false, true},
		{"already set up, window asked for", wizardState{}, set, none, main, false, true},
		{"helper unknown yet", wizardState{}, snapHelper(status.HelperUnknown, nil), none, launchOpen{}, false, false},
		{"scratch helper", wizardState{}, fresh, scratch, launchOpen{}, false, true},
		{"scratch helper, window asked for", wizardState{}, fresh, scratch, main, true, true},
		{"scratch helper, a page asked for", wizardState{}, fresh, scratch, launchOpen{main: true, page: pageKernel}, false, true},
		{"scratch helper, forced", wizardState{}, fresh, env(map[string]string{"TWINNEL_SOCKET": "/tmp/x.sock", "TWINNEL_WIZARD": "1"}), launchOpen{}, true, true},
		{"turned off", wizardState{}, fresh, env(map[string]string{"TWINNEL_WIZARD": "0"}), main, false, true},
	}
	for _, c := range cases {
		open, decided := launchOpensWizard(c.st, c.s, c.getenv, c.req)
		if open != c.open || decided != c.decided {
			t.Errorf("%s: open %v decided %v, want %v %v", c.name, open, decided, c.open, c.decided)
		}
	}
}

// 继续设置 stays while the machine is not set up, or the wizard was left
// before 完成.
func TestWizardPending(t *testing.T) {
	a := &app{}
	if !a.wizardPending(snapHelper(status.HelperNotInstalled, nil)) {
		t.Error("no helper: not pending")
	}
	set := snapHelper(status.HelperConnected, &rpc.Status{ActiveSubscription: "main"})
	if a.wizardPending(set) {
		t.Error("set up, wizard never run: pending")
	}
	a.wiz.state = wizardState{Step: wizTS}
	if !a.wizardPending(set) {
		t.Error("left at Tailscale: not pending")
	}
	a.wiz.state.Done = true
	if a.wizardPending(set) {
		t.Error("finished: pending")
	}
}

// A closed administrator prompt is said in words, in the UI language.
func TestHelperFailure(t *testing.T) {
	if got := helperFailure("plan…\ntwinnel: cancelled; nothing was changed\n"); got != i18n.T.WizHelperCancelled {
		t.Errorf("cancelled: %q", got)
	}
	if got := helperFailure("twinnel: helper install did not complete"); got != "helper install did not complete" {
		t.Errorf("other: %q", got)
	}
}

// Each step's "done" follows the flow (and, for the proxy, a takeover).
func TestWizardStepsDone(t *testing.T) {
	a := &app{shotMode: true}
	steps := func(done ...setup.Step) []setup.StepState {
		var out []setup.StepState
		for _, id := range setup.Order {
			st := setup.StepState{ID: id, State: setup.Pending}
			for _, d := range done {
				if d == id {
					st.State = setup.Done
				}
			}
			out = append(out, st)
		}
		return out
	}
	s := snapHelper(status.HelperNotInstalled, nil)
	a.setupV.shotSteps = steps()
	a.wiz.shot = &wizardShot{}
	for _, step := range []int{wizHelper, wizMihomo, wizSub, wizTS} {
		if a.wizStepDone(step, s) {
			t.Errorf("step %d done on a fresh machine", step)
		}
	}
	a.setupV.shotSteps = steps(setup.StepHelper, setup.StepMihomo, setup.StepSubscription)
	if !a.wizStepDone(wizHelper, s) || !a.wizStepDone(wizMihomo, s) {
		t.Error("helper / kernel done not seen")
	}
	if a.wizStepDone(wizSub, s) {
		t.Error("subscription done before the proxy started")
	}
	a.setupV.shotSteps = steps(setup.StepHelper, setup.StepMihomo, setup.StepSubscription, setup.StepApply)
	if !a.wizStepDone(wizSub, s) {
		t.Error("proxy started, ④ not done")
	}
	a.wiz.state.TSSkipped = true
	if !a.wizStepDone(wizTS, s) {
		t.Error("skipped Tailscale blocks ⑤")
	}
	a.setupV.shotSteps, a.wiz.shot = steps(), &wizardShot{takenOver: true}
	if !a.wizStepDone(wizMihomo, s) || !a.wizStepDone(wizSub, s) {
		t.Error("a takeover does not count for ③ and ④")
	}
}
