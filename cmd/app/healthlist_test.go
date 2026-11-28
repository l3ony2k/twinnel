package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
)

// healthTester draws the health page of a report holding checks, on
// goos's platform.
func healthTester(t *testing.T, goos string, checks []health.CheckReport) *ui.Tester {
	t.Helper()
	usePlat(t, goos)
	a, snap := layoutApp(t)
	rep := *snap.Health.Report
	rep.Checks = checks
	snap.Health.Report = &rep
	tr := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { a.healthPage(c, snap) })
	}, 1000, 1400)
	tr.Frame()
	tr.Frame()
	return tr
}

func okCheck(id string) health.CheckReport {
	return health.CheckReport{ID: id, Title: "check " + id, Result: health.Result{Status: health.StatusOK, Summary: "fine"}}
}

// 6.1: the passed checks' group is 其他检查 only after another group;
// alone it is 全部检查.
func TestHealthPassedGroupTitle(t *testing.T) {
	defer i18n.Set("zh")
	i18n.Set("zh")
	T := i18n.T
	all := []health.CheckReport{okCheck("a"), okCheck("b")}
	tr := healthTester(t, "windows", all)
	if !tr.HasText(T.HealthAll) || tr.HasText(T.HealthPassed) {
		t.Errorf("all passing: %q", tr.Texts())
	}
	warn := okCheck("w")
	warn.Status, warn.Summary = health.StatusWarn, "not fine"
	tr = healthTester(t, "windows", append(all, warn))
	if tr.HasText(T.HealthAll) || !tr.HasText(T.HealthPassed) {
		t.Errorf("with a problem: %q", tr.Texts())
	}
}

// 6.2/6.3: on Windows a check's title is in the body weight and a skipped
// check's reason is plain secondary text, no bordered chip; macOS keeps
// the medium title and the chip.
func TestHealthRowFluent(t *testing.T) {
	defer i18n.Set("zh")
	i18n.Set("zh")
	skip := okCheck("s")
	skip.Status, skip.Summary = health.StatusSkip, "skipped-reason"
	for goos, want := range map[string]struct {
		weight int
		chip   bool
	}{"windows": {400, false}, "darwin": {500, true}} {
		tr := healthTester(t, goos, []health.CheckReport{okCheck("a"), skip})
		if got := healthRowWeight(); got != want.weight {
			t.Errorf("%s: title weight %d", goos, got)
		}
		r, ok := tr.Find("skipped-reason")
		if !ok {
			t.Fatalf("%s: no reason in %q", goos, tr.Texts())
		}
		// A chip's border: a line of the border color just outside the
		// text's box, above it.
		img := tr.Image()
		bg := pixel(img, r.X+r.W/2, r.Y-12)
		chip := false
		for y := r.Y - 4; y < r.Y; y++ {
			if p := pixel(img, r.X+r.W/2, y); contrast(p, bg) > 1.15 {
				chip = true
			}
		}
		if chip != want.chip {
			t.Errorf("%s: chip %v, want %v", goos, chip, want.chip)
		}
	}
}
