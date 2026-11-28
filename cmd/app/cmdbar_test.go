package main

import (
	"testing"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// N3 (Windows QA round 5): at the window's minimum width 总览's
// CommandBar stays one line: the commands that do not fit fold into "…"
// (last first) instead of wrapping; at the default width they show.
func TestOverviewCommandBarFolds(t *testing.T) {
	t.Cleanup(func() { i18n.Set("zh") })
	for _, lang := range []string{"zh", "en"} {
		i18n.Set(lang)
		T := i18n.T
		for _, w := range []int{760, 1280} {
			a, tr := shellTester(t, "windows", w, 800)
			for range 4 {
				tr.Frame()
			}
			first, ok1 := accessNamed(tr, T.TestAllDelay)
			more, ok2 := accessNamed(tr, T.OvMoreActions)
			if !ok1 || !ok2 {
				t.Fatalf("%s %d: commands not found (%v %v)", lang, w, ok1, ok2)
			}
			if first.Bounds.Y != more.Bounds.Y {
				t.Errorf("%s %d: the bar wrapped: 全部测速 at y=%v, … at y=%v", lang, w, first.Bounds.Y, more.Bounds.Y)
			}
			if right := more.Bounds.X + more.Bounds.W; right > a.paneBox.X+a.paneBox.W {
				t.Errorf("%s %d: … ends at %v past the pane %+v", lang, w, right, a.paneBox)
			}
			_, rec := accessNamed(tr, T.Recovery)
			t.Logf("%s %d: shown %d, recovery button %v", lang, w, a.ovBar.shown, rec)
			if w == 1280 && a.ovBar.shown != 3 {
				t.Errorf("%s %d: %d commands shown, want all 3", lang, w, a.ovBar.shown)
			}
		}
	}
}
