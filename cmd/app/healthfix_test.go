package main

import (
	"testing"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
)

// TestEveryReasonHasARowAction: each reason, on each platform, gives the
// row a plain sentence and an action the app knows: a one-click fix's
// action, a guided fix's page, or a manual fix's explanation.
func TestEveryReasonHasARowAction(t *testing.T) {
	for _, tag := range []string{"zh-Hans", "en"} {
		i18n.Set(tag)
		for _, r := range health.Reasons() {
			for _, p := range health.AllPlatforms {
				cr := health.CheckReport{ID: "x", Result: health.Result{Status: health.StatusFail, Reasons: []string{r}}}
				fs := rowFixes(cr, p)
				if len(fs) != 1 || fs[0].text.Plain == "" || fs[0].text.How == "" {
					t.Fatalf("%s %s@%s: row fixes %+v", tag, r, p, fs)
				}
				switch f := fs[0]; f.spec.Kind {
				case health.FixAuto:
					if f.spec.Action != health.FixRerun && f.spec.Action != health.FixEgressProbe &&
						!needsHelperAction(f.spec.Action) && f.spec.Action != health.FixModeRule && f.spec.Action != health.FixExitNodeOff {
						t.Errorf("%s@%s: unknown action %s", r, p, f.spec.Action)
					}
				case health.FixGuided:
					if guidedPage(f.spec.Target) == "" {
						t.Errorf("%s@%s: no page for target %s", r, p, f.spec.Target)
					}
				}
			}
		}
	}
	i18n.Set("zh-Hans")
}

func TestRowFixesDedupe(t *testing.T) {
	cr := health.CheckReport{Result: health.Result{Reasons: []string{health.ReasonLeakSelected, health.ReasonLeakMayGo, health.ReasonLeakNoGroup}}}
	if n := len(rowFixes(cr, health.Darwin)); n != 2 {
		t.Fatalf("got %d fixes, want 2 (one leak guard fix, one guided)", n)
	}
	if rowFixLine(cr, health.Darwin) == "" {
		t.Fatal("no plain line")
	}
}
