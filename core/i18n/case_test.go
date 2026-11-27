package i18n

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
)

// English case (docs/i18n-glossary.md, R-unslop-25): buttons, menu items,
// page and window titles and the sidebar in title case; section and card
// headings, row labels, steps, messages and toasts in sentence case.

// headingKeys are the texts shown as a section or card heading, or as a
// step of 入门.
var headingKeys = []string{
	"AboutBasics", "AboutTakeover", "AbtTkChanges", "AbtTkEdit", "AbtTkUndo", "AbtTkWhenNot",
	"Components", "General", "HelperSection", "Language", "Layers", "Links", "ManagedTitle",
	"MenuBar", "Notifications", "Traffic", "GettingStarted", "GetStarted",
	"StepHelper", "StepCheckCoexist", "StepTakeOver",
	"TakeoverFound", "TakeoverPlans", "TakeoverResult",
	"LeaveUndo", "LeaveUninstall", "LeaveRemoveAll", "LeaveExport",
}

// properWords keep their capital inside a sentence.
var properWords = map[string]bool{
	"duet": true, "mihomo": true, "Tailscale": true, "TUN": true, "DNS": true, "GEO": true,
	"MagicDNS": true, "IP": true, "Mac": true, "GitHub": true, "DERP": true, "I": true,
}

func TestHeadingsSentenceCase(t *testing.T) {
	v := reflect.ValueOf(en)
	for _, k := range headingKeys {
		f := v.FieldByName(k)
		if !f.IsValid() {
			t.Errorf("%s: no such text", k)
			continue
		}
		words := strings.Fields(f.String())
		for _, w := range words[1:] {
			w = strings.Trim(w, "()…,:")
			if w == "" || properWords[w] {
				continue
			}
			if unicode.IsUpper([]rune(w)[0]) {
				t.Errorf("%s: %q is not sentence case (%q)", k, f.String(), w)
				break
			}
		}
	}
}

// One name per action: the helper's install button and page title.
func TestInstallHelperName(t *testing.T) {
	if en.OnboardInstall != "Install Helper" || en.OnboardTitle != en.OnboardInstall || en.FixInstallHelper != en.OnboardInstall {
		t.Errorf("install helper: %q, %q, %q", en.OnboardInstall, en.OnboardTitle, en.FixInstallHelper)
	}
}
