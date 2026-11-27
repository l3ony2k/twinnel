package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/msg"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/update"
)

// otherLang returns what in texts belongs to the other UI language:
// English words (not names kept as written) in zh, Chinese in en.
func otherLang(lang string, texts []string) []string {
	var bad []string
	for _, s := range texts {
		if lang == "en" {
			if i18n.HasHan(s) {
				bad = append(bad, s)
			}
		} else if w := i18n.EnglishWords(s); len(w) > 0 {
			bad = append(bad, fmt.Sprintf("%s (%s)", s, strings.Join(w, " ")))
		}
	}
	return bad
}

// The takeover result and the update check show nothing in the other
// language (walkthrough 2, N1 and N15): the helper's English detail is
// worded from data or kept behind 复制, and a reason is worded when drawn.
func TestTakeoverResultAndUpdateInOneLanguage(t *testing.T) {
	defer i18n.Set("zh-Hans")
	r := &rpc.AdoptResult{
		OK: true,
		Steps: []rpc.AdoptStep{
			{Name: "backup", Code: "backup", Status: "ok", Detail: "copied to /tmp/x"},
			{Name: "apply", Code: "apply", Status: "ok", Detail: "written=true reloaded=true"},
		},
		Apply: &rpc.ApplyResult{Impact: &rpc.ReloadImpact{Disruptive: true, TUNRebuild: true, TUNWhy: []string{"tun"},
			Notes: []string{"the TUN device is closed and created again (tun section changed)"}}},
		Warnings: []string{
			"this reload interrupts the network briefly: the TUN device is closed and created again (tun section changed): routes are re-added",
			"the applied config differs from the plan's composed.yaml (the tailnet peers or interface changed since the plan)",
			"generated a new controller secret",
			"the mihomo binary /opt/x/mihomo can be replaced without root (owned by uid 501), and the service runs it as root",
		},
	}
	errs := []error{
		update.ErrRateLimited,
		errors.New(`Get "https://api.github.com/repos/l3ony2k/twinnel/releases/latest": dial tcp: lookup api.github.com: no such host`),
		errors.New("something nobody words"),
	}
	for _, lang := range []string{"zh-Hans", "en"} {
		i18n.Set(lang)
		T := i18n.T
		var texts []string
		for _, st := range r.Steps {
			texts = append(texts, stepName(st), stepPhrase(st))
		}
		texts = append(texts, i18n.F(T.TkDoneNeedsWork, 2), T.TakeoverDone)
		for _, n := range adoptNotes(r) {
			if n.kind == noteInfo {
				texts = append(texts, n.text)
				continue
			}
			text, raw := T.MsgText(msg.Parse(n.text), true)
			if raw {
				t.Errorf("%s: warning %q has no words", lang, n.text)
			}
			texts = append(texts, text)
		}
		for _, set := range []bool{true, false} {
			texts = append(texts, adoptSecretText(r, set), adoptSecretText(&rpc.AdoptResult{}, set))
		}
		for _, err := range errs {
			texts = append(texts, updateFailText(err))
		}
		texts = append(texts, T.UpdateChecking, T.UpdateLatest, T.UpdateNone, i18n.F(T.UpdateNewVersion, "0.2.0"))
		for _, b := range otherLang(lang, texts) {
			t.Errorf("%s: %s", lang, b)
		}
		if strings.Contains(strings.Join(texts, "\n"), "composed.yaml") {
			t.Errorf("%s: composed.yaml named", lang)
		}
	}
}
