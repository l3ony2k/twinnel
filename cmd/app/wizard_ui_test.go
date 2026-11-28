package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/setup"
)

// renderWizShot draws the wizard state named name (wizShotList) on goos's
// UI path, headless, and returns the Tester.
func renderWizShot(t *testing.T, goos, name string) *ui.Tester {
	t.Helper()
	old := plat
	t.Cleanup(func() { plat = old; resetFlags() })
	plat = platformFor(goos)
	resetFlags()
	a, _ := layoutApp(t)
	tr := ui.NewTesterWith(func(c *ui.Context) { a.mainView(c) }, 1100, 820, ui.TesterOptions{Dark: true})
	tr.SetTitleBar(titleBarFor(goos))
	for _, sh := range wizShotList(goos) {
		if sh.name != name {
			continue
		}
		prev := a.page
		a.page = pageWizard
		if sh.page != "" {
			a.page = sh.page
		}
		a.pageChanged(prev)
		a.hd.onb = onboardingView{loaded: true}
		a.applyWizShot(sh, goos)
		tr.Frame()
		tr.Frame()
		return tr
	}
	t.Fatalf("no wizard shot %q", name)
	return nil
}

func button(t *testing.T, tr *ui.Tester, name string) ui.AccessInfo {
	t.Helper()
	b, ok := tr.AccessByRole(ui.RoleButton, name)
	if !ok {
		t.Fatalf("no %q button; texts %q", name, tr.Texts())
	}
	return b
}

// 1.1: ③ left for the subscription's nodes is no dead end: 下一步 goes
// on, a way back to the direct download shows, and the stepper does not
// tick a kernel that is not there yet.
func TestWizardViaSubGoesOn(t *testing.T) {
	T := i18n.T
	for _, goos := range []string{"windows", "darwin", "linux"} {
		tr := renderWizShot(t, goos, "sub-download-later")
		if b := button(t, tr, T.WizNext); b.Disabled {
			t.Errorf("%s: 下一步 disabled while ③ waits for ④", goos)
		}
		button(t, tr, T.WizMihomoDirect)
	}
	a := &app{shotMode: true}
	a.wiz.shot = &wizardShot{}
	a.setupV.shotSteps = []setup.StepState{{ID: setup.StepHelper, State: setup.Done}}
	a.wiz.viaSub = true
	s := snapHelper(0, nil)
	if !a.wizStepDone(wizMihomo, s) || a.wizStepComplete(wizMihomo, s) {
		t.Error("viaSub: ③ must pass (wizStepDone) without being complete")
	}
}

// 1.2: a failed row is Fluent's InfoBar Error on Windows (error glyph in
// the critical color, title in the text color); elsewhere the caution mark.
func TestFailedMarkPerPlatform(t *testing.T) {
	old := plat
	defer func() { plat = old }()
	th := ui.DarkTheme()
	plat = platformFor("windows")
	if icon, col, title := failedMark(th); icon != iconError || col != th.Danger || title != th.Text {
		t.Error("windows: failed row not drawn as an error")
	}
	for _, goos := range []string{"darwin", "linux"} {
		plat = platformFor(goos)
		if icon, col, _ := failedMark(th); icon != iconWarning || col != th.Warning {
			t.Errorf("%s: failed row changed its mark", goos)
		}
	}
}

// 1.3: a skipped optional step is not "left", and the heading waits for
// the checks instead of turning to 还有 n 项.
func TestWizardDoneHeading(t *testing.T) {
	T := i18n.T
	cases := []struct {
		left    int
		checked bool
		issues  int
		want    wizDoneState
		head    string
	}{
		{0, false, 0, wizDoneChecking, T.WizDoneChecking},
		{0, true, 0, wizDoneAllSet, T.WizDoneHead},
		{0, true, 2, wizDoneLeft, i18n.F(T.WizDoneIssues, 2)},
		{1, false, 0, wizDoneLeft, i18n.F(T.WizDoneIssues, 1)},
		{1, true, 2, wizDoneLeft, i18n.F(T.WizDoneIssues, 3)},
	}
	for _, c := range cases {
		head, st := wizDoneHeading(c.left, c.checked, c.issues)
		if st != c.want || head != c.head {
			t.Errorf("%+v: %q %v", c, head, st)
		}
	}
	tr := renderWizShot(t, "windows", "done-checking")
	if _, ok := tr.Find(T.WizDoneChecking); !ok {
		t.Errorf("done-checking: heading not 正在检查; texts %q", tr.Texts())
	}
}

// 1.4: DNS set by hand has its way: how (ⓘ) and the address to copy.
func TestWizardDNSManualAction(t *testing.T) {
	tr := renderWizShot(t, "windows", "done-dns-manual")
	button(t, tr, i18n.T.WizDoneDNSCopy)
}

// 1.5 and 1.6: Tailscale's busy button keeps its width; a failed install
// keeps the official download beside 重试.
func TestWizardTSStates(t *testing.T) {
	T := i18n.T
	tr := renderWizShot(t, "windows", "ts-installing")
	if b := button(t, tr, T.WizTSInstalling); b.Bounds.W > 300 {
		t.Errorf("installing button %v DIP wide: stretched across the card", b.Bounds.W)
	}
	tr = renderWizShot(t, "windows", "ts-failed")
	if _, ok := tr.Find(T.TSLDownload); !ok {
		t.Errorf("ts-failed: no %q; texts %q", T.TSLDownload, tr.Texts())
	}
}

// 1.7: a damaged program is said in words: no path, no helper English.
func TestSetupReasonDamaged(t *testing.T) {
	st := setup.StepState{ID: setup.StepTailscale, State: setup.Failed, Code: setup.CodeDamaged,
		Error: `Tailscale is installed but its files are damaged: C:\Program Files\Tailscale\tailscale.exe (zeroed); repair it to reinstall`}
	r := setupReason(st)
	if r == "" || strings.ContainsAny(r, `\/()（）`) || strings.Contains(r, "zeroed") {
		t.Errorf("damaged reason %q", r)
	}
}

// 1.8: each step says what it is in one sentence; the rest is behind ⓘ.
func TestWizardStepBodiesOneSentence(t *testing.T) {
	for _, l := range i18n.Langs() {
		i18n.Set(l.Tag)
		T := i18n.T
		for _, body := range []string{T.WizHelperBody, T.WizMihomoBody, T.WizSubBody, T.WizTSBody} {
			n := strings.Count(body, "。") + strings.Count(body, ". ") + strings.Count(body, "；") + strings.Count(body, "; ")
			if n > 1 || (strings.Count(body, "。") == 1 && !strings.HasSuffix(body, "。")) {
				t.Errorf("%s: more than one sentence: %q", l.Tag, body)
			}
		}
	}
	i18n.Set(i18n.Langs()[0].Tag)
}

// 1.9 and 1.10: on Windows the step counter is gone, the footer's three
// buttons share one Fluent height and 稍后再说 sits at the right end;
// macOS keeps its counter and Setup Assistant's 稍后再说 at the left.
func TestWizardChromePerPlatform(t *testing.T) {
	T := i18n.T
	counter := i18n.F(T.WizProgress, 3, wizSteps)
	tr := renderWizShot(t, "windows", "mihomo")
	if slices.Contains(tr.Texts(), counter) {
		t.Error("windows: the step counter repeats the stepper")
	}
	later, back, next := button(t, tr, T.WizLater), button(t, tr, T.WizBack), button(t, tr, T.WizNext)
	for _, b := range []ui.AccessInfo{later, back, next} {
		if b.Bounds.H != wizFooterButtonH {
			t.Errorf("windows: %q is %v high, want %d", b.Name, b.Bounds.H, wizFooterButtonH)
		}
	}
	if !(back.Bounds.X < next.Bounds.X && next.Bounds.X < later.Bounds.X) {
		t.Errorf("windows: footer order %v %v %v, want 上一步 · 下一步 · 稍后再说", back.Bounds.X, next.Bounds.X, later.Bounds.X)
	}
	tr = renderWizShot(t, "darwin", "mihomo")
	if !slices.Contains(tr.Texts(), counter) {
		t.Error("darwin: the step counter went")
	}
	if l, b := button(t, tr, T.WizLater), button(t, tr, T.WizBack); l.Bounds.X > b.Bounds.X {
		t.Error("darwin: 稍后再说 left the footer's left end")
	}
}

// 1.11: the plan's list is never an empty box.
func TestInstallPlanListNeverEmpty(t *testing.T) {
	tr := renderWizShot(t, "windows", "helper-changes")
	if _, ok := tr.Find(i18n.T.WizPlanEmpty); !ok {
		t.Errorf("helper-changes: empty plan box; texts %q", tr.Texts())
	}
}

// 1.12: the way to ② is the accent; ④'s field has a visible label and
// goes after an export's import; the found config's path is one line.
func TestWizardSmallFixes(t *testing.T) {
	T := i18n.T
	tr := renderWizShot(t, "windows", "sub")
	if _, ok := tr.Find(T.WizSubURL); !ok {
		t.Error("sub: no visible label over the link field")
	}
	tr = renderWizShot(t, "windows", "sub-imported")
	if _, ok := tr.AccessByRole(ui.RoleTextField, T.WizSubURL); ok {
		t.Error("sub-imported: the link field stays after an export's import")
	}
	if wizSubURLShown("a.zip", setup.StepState{State: setup.Pending}) || !wizSubURLShown("a.zip", setup.StepState{State: setup.Waiting}) || !wizSubURLShown("Merge.yaml", setup.StepState{}) {
		t.Error("wizSubURLShown")
	}
	tr = renderWizShot(t, "windows", "mihomo-found")
	if _, ok := tr.Find(`C:\Users\example\.config\mihomo\config.yaml`); !ok {
		t.Errorf("mihomo-found: the path is not one line of its own; texts %q", tr.Texts())
	}
}
