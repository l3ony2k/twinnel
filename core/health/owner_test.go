package health

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/foreign"
)

func runAll(t *testing.T, env Env, lang string) *Report {
	t.Helper()
	return (&Runner{Registry: DefaultRegistry()}).Run(WithLang(context.Background(), lang), env)
}

func byID(rep *Report) map[string]CheckReport {
	m := map[string]CheckReport{}
	for _, c := range rep.Checks {
		m[c.ID] = c
	}
	return m
}

// TestEvidenceSource: every check that read the running mihomo names the
// controller it read.
func TestEvidenceSource(t *testing.T) {
	rep := runAll(t, healthyEnv(), "zh-Hans")
	if rep.Controller == nil || rep.Controller.Verdict != foreign.Managed {
		t.Fatalf("report controller %+v", rep.Controller)
	}
	c := byID(rep)
	for _, id := range []string{"tun-route-exclude", "tailscale-direct-rules", "magicdns", "leak-guard"} {
		if c[id].Controller != "127.0.0.1:9090（Twinnel）" {
			t.Errorf("%s: controller %q", id, c[id].Controller)
		}
		if c[id].Status != StatusOK {
			t.Errorf("%s: %s %s", id, c[id].Status, c[id].Summary)
		}
	}
	// fake-ip-filter-mode reads the config file only.
	if c["fake-ip-filter-mode"].Controller != "" {
		t.Errorf("fake-ip-filter-mode controller %q", c["fake-ip-filter-mode"].Controller)
	}
	if c["controller-owner"].Status != StatusOK {
		t.Errorf("controller-owner %+v", c["controller-owner"])
	}
}

// TestForeignNotJudged: with Clash Party's mihomo on the controller, the
// checks that would read its config are not judged, and the owner check
// fails with both reasons (report pit #16).
func TestForeignNotJudged(t *testing.T) {
	env := foreignEnv().(*fakeEnv)
	// What Clash Party's mihomo would report: no Tailscale rules at all.
	env.rt.Rules = []MihomoRule{{Type: "Match", Target: "PROXY"}}
	for _, lang := range []string{"zh-Hans", "en"} {
		rep := runAll(t, env, lang)
		c := byID(rep)
		o := c["controller-owner"]
		if o.Status != StatusFail || len(o.Reasons) != 2 || !strings.Contains(o.Summary, "Clash Party") || o.Fix == nil || o.Fix.AutoFix != FixControllerPort {
			t.Fatalf("%s owner: %+v", lang, o)
		}
		for _, id := range []string{"tun-route-exclude", "tailscale-direct-rules", "leak-guard"} {
			r := c[id]
			if r.Status != StatusSkip || !strings.Contains(r.Summary, "Clash Party") || !strings.Contains(r.Controller, "Clash Party") || len(r.Reasons) != 0 {
				t.Errorf("%s %s: %s %q controller %q reasons %v", lang, id, r.Status, r.Summary, r.Controller, r.Reasons)
			}
		}
		if lang == "zh-Hans" {
			if w := c["tailscale-direct-rules"].Summary; !strings.Contains(w, "不判断") {
				t.Errorf("zh summary %q", w)
			}
		}
	}
}

func TestWithOwnerKeepsEgress(t *testing.T) {
	env := WithOwner(healthyEnv(), func(context.Context) foreign.Owner { return foreign.Owner{Addr: "x:1", Verdict: foreign.Managed} })
	if _, ok := env.(EgressEnv); !ok {
		t.Fatal("egress lost")
	}
	if _, ok := env.(OwnerEnv); !ok {
		t.Fatal("owner lost")
	}
	if a, err := env.(EgressEnv).DirectEgress(context.Background(), DefaultEgressURLs[0]); err != nil || !a.IsValid() {
		t.Error(a, err)
	}
	rep := runAll(t, env, "en")
	if c := byID(rep)["leak-guard"]; c.Controller != "x:1 (Twinnel)" {
		t.Errorf("leak-guard controller %q", c.Controller)
	}
}

// The owner wrappers keep an Env without the egress test unsupported.
func TestWithOwnerKeepsUnsupportedEgress(t *testing.T) {
	env := healthyEnv()
	env.platform = Windows
	env.settings.Egress = true
	wrapped := WithOwner(noEgress{env}, func(context.Context) foreign.Owner { return foreign.Owner{Addr: "x:1", Verdict: foreign.Managed} })
	rep := (&Runner{Registry: DefaultRegistry(), Only: []string{"leak-guard-egress"}}).Run(context.Background(), wrapped)
	wantUnsupported(t, rep.Checks[0].Result, "owner-wrapped")
}

// The owner wrappers keep the dataplane probes of the wrapped Env.
func TestWithOwnerKeepsDataplane(t *testing.T) {
	env := WithOwner(NetProbesEnv{healthyEnv()}, func(context.Context) foreign.Owner { return foreign.Owner{Addr: "x:1", Verdict: foreign.Managed} })
	if _, ok := env.(DataplaneEnv); !ok {
		t.Fatal("dataplane lost")
	}
	tr := &trackEnv{Env: env}
	if err := tr.DialTCP(context.Background(), "192.0.2.1:1"); err != errDialMarker {
		t.Errorf("DialTCP not forwarded: %v", err)
	}
	if tr.used() {
		t.Error("a plain dial counted as reading mihomo")
	}
}

var errDialMarker = fmt.Errorf("dial marker")

// NetProbesEnv is an Env whose DialTCP answers errDialMarker.
type NetProbesEnv struct{ *fakeEnv }

func (NetProbesEnv) DialTCP(context.Context, string) error { return errDialMarker }
func (NetProbesEnv) DialViaProxy(context.Context, string, string, string) error {
	return errDialMarker
}
func (NetProbesEnv) Ping(context.Context, string) error { return errDialMarker }

// TestForeignUnknownClientWording: a foreign mihomo of no known client is
// named once, with no stray spaces around the Chinese name, and no check
// renders an empty evidence line (QA round 2, E).
func TestForeignUnknownClientWording(t *testing.T) {
	e := foreignEnv().(*fakeEnv)
	e.owner.Client, e.owner.Exe, e.owner.WorkDir = "", "", ""
	rep := runAll(t, e, "zh-Hans")
	o := byID(rep)["controller-owner"]
	if o.Summary != "127.0.0.1:9090 被另一个 mihomo 占用" {
		t.Errorf("summary %q", o.Summary)
	}
	if o.Fix == nil || !strings.HasPrefix(o.Fix.Text, "退出另一个 mihomo（") {
		t.Errorf("fix %+v", o.Fix)
	}
	for _, c := range rep.Checks {
		if strings.Contains(c.Summary, "被 另") || strings.Contains(c.Controller, "（ ") {
			t.Errorf("%s: stray space: %q / %q", c.ID, c.Summary, c.Controller)
		}
		for _, ev := range c.Evidence {
			for _, l := range strings.Split(ev, "\n") {
				if strings.TrimSpace(l) == "" {
					t.Errorf("%s: empty evidence line in %q", c.ID, c.Evidence)
				}
			}
		}
	}
	if txt := rep.Text(); strings.Contains(txt, "· \n") {
		t.Errorf("empty evidence line in the text report:\n%s", txt)
	}
}

func TestTidyEvidence(t *testing.T) {
	got := TidyEvidence([]string{"", "  ", "a\n\nb  \n", "\n", "c"})
	if strings.Join(got, "|") != "a\nb|c" {
		t.Errorf("TidyEvidence = %q", got)
	}
}
