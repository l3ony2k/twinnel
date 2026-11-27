package i18n

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// wordedFor returns a copy of a language's table worded for goos, as
// that system's init would leave it (desktop.go); the real tables stay
// this host's.
func wordedFor(t *testing.T, l Lang, goos string) *Strings {
	t.Helper()
	c := *l.S
	cloneMaps(&c)
	if goos == Darwin {
		return &c
	}
	w := map[string]map[string]func(*Strings){Linux: linuxWording, Windows: windowsWording}[goos]
	fn := w[l.Tag]
	if fn == nil {
		t.Fatalf("%s: no %s wording", l.Tag, goos)
	}
	fn(&c)
	return &c
}

// TestPlatformGlossary: on each system, no text the app shows there
// names another system's thing (glossary.go). The wizard's per-system
// words are checked in TestWizardOSWording.
func TestPlatformGlossary(t *testing.T) {
	var bad []string
	for _, goos := range Platforms {
		for _, l := range Langs() {
			walkStrings(reflect.ValueOf(wordedFor(t, l, goos)).Elem(), "", func(path, s string) {
				if !ShownOn(path, goos) {
					return
				}
				if f := ForeignTerms(goos, s); len(f) > 0 {
					bad = append(bad, goos+" "+l.Tag+" "+path+" ["+strings.Join(f, ", ")+"]: "+s)
				}
			})
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}
