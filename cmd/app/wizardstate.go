package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/l3ony2k/twinnel/core/status"
)

// The first-run wizard's memory and its way in: what it keeps across
// launches (setup.json next to app.json), whether this machine still
// needs setting up, and whether a launch opens the wizard by itself.

// Wizard steps, in order.
const (
	wizWelcome = iota
	wizHelper
	wizMihomo
	wizSub
	wizTS
	wizDone
	wizSteps // how many
)

// wizardState is what the wizard keeps across launches.
type wizardState struct {
	// Step is the step shown last: the wizard resumes there.
	Step int `json:"step"`
	// Later: 稍后再说 was chosen. A launch no longer opens the wizard by
	// itself; 继续设置 (总览, the helper pages, the flyout) brings it back.
	Later bool `json:"later,omitempty"`
	// Done: 完成 was clicked on the last step.
	Done bool `json:"done,omitempty"`
	// TSSkipped: Tailscale was skipped.
	TSSkipped bool `json:"tailscale_skipped,omitempty"`
}

// wizardStatePath is setup.json in the app's config folder.
func wizardStatePath(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), "setup.json")
}

// loadWizardState reads the wizard's state; a missing or broken file is a
// fresh start.
func loadWizardState(path string) wizardState {
	var st wizardState
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &st) != nil {
		return wizardState{}
	}
	st.Step = min(max(st.Step, 0), wizSteps-1)
	return st
}

// saveWizardState writes it (a temporary file, then a rename).
func saveWizardState(path string, st wizardState) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// needsSetup reports whether Twinnel is not set up on this machine yet:
// no helper, or a helper that has neither taken over a mihomo nor applied
// a config of its own. known is false while that cannot be told yet (the
// helper's state is still being read). A helper that is installed but
// down is not a first run: its own state row deals with it.
func needsSetup(s status.Snapshot) (need, known bool) {
	switch s.Helper.State {
	case status.HelperNotInstalled:
		return true, true
	case status.HelperConnected:
		if s.Helper.Status == nil {
			return false, false
		}
		return !s.TakenOver(), true
	case status.HelperUnknown:
		return false, false
	}
	return false, true
}

// wizardAutoOpen reports whether a launch may open the wizard by itself.
// TWINNEL_WIZARD=1 or 0 decides; otherwise a run against a scratch helper
// (TWINNEL_SOCKET: a development or agent run) never opens a window on its
// own, so it never takes the focus of the person at the machine.
func wizardAutoOpen(getenv func(string) string) bool {
	switch getenv("TWINNEL_WIZARD") {
	case "1":
		return true
	case "0":
		return false
	}
	return getenv("TWINNEL_SOCKET") == ""
}

// wizardLaunchCheck opens the wizard once per launch, as soon as the
// helper's state is known, when this machine is not set up and the
// wizard was neither finished nor put off. Main thread.
func (a *app) wizardLaunchCheck(s status.Snapshot) {
	w := &a.wiz
	if w.checked || a.shotMode {
		return
	}
	open, decided := launchOpensWizard(w.state, s, os.Getenv)
	if !decided {
		return
	}
	w.checked = true
	if open {
		a.openWizard()
	}
}

// launchOpensWizard decides whether this launch opens the wizard; decided
// is false until the helper's state is known.
func launchOpensWizard(st wizardState, s status.Snapshot, getenv func(string) string) (open, decided bool) {
	need, known := needsSetup(s)
	if !known {
		return false, false
	}
	return need && !st.Done && !st.Later && wizardAutoOpen(getenv), true
}

// wizardPending: the 继续设置 entry shows. The machine still needs setting
// up, or the wizard was started and not finished (Tailscale may be left).
func (a *app) wizardPending(s status.Snapshot) bool {
	if need, known := needsSetup(s); known && need {
		return true
	}
	st := a.wiz.state
	return st.Step > wizWelcome && !st.Done && s.Helper.State != status.HelperUnknown
}

// setWizardStep shows step and remembers it.
func (a *app) setWizardStep(step int) {
	w := &a.wiz
	w.state.Step = min(max(step, 0), wizSteps-1)
	if w.state.Step == wizDone {
		a.rerunHealth() // ⑥ shows the checks as they are now
	}
	a.saveWizard()
	a.invalidate()
}

func (a *app) saveWizard() {
	if a.shotMode {
		return
	}
	if err := saveWizardState(a.wiz.path, a.wiz.state); err != nil {
		a.toastErr = err.Error()
	}
}
