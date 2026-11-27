package health

import (
	"context"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/l3ony2k/twinnel/core/i18n"
)

var verbRe = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z%]`)

// Every table has every text, with the format verbs of the Chinese one.
func TestTextsComplete(t *testing.T) {
	ref := reflect.ValueOf(zhTexts)
	for tag, tab := range textTables {
		v := reflect.ValueOf(*tab)
		for i := range ref.NumField() {
			name := ref.Type().Field(i).Name
			want, got := ref.Field(i).String(), v.Field(i).String()
			if got == "" {
				t.Errorf("%s: %s missing", tag, name)
				continue
			}
			if a, b := verbRe.FindAllString(want, -1), verbRe.FindAllString(got, -1); !slices.Equal(a, b) {
				t.Errorf("%s: %s has verbs %v, want %v", tag, name, b, a)
			}
		}
	}
}

// No Han characters in the English table.
func TestEnglishHasNoChinese(t *testing.T) {
	v := reflect.ValueOf(enTexts)
	for i := range v.NumField() {
		for _, r := range v.Field(i).String() {
			if unicode.Is(unicode.Han, r) {
				t.Errorf("%s: %q", v.Type().Field(i).Name, v.Field(i).String())
				break
			}
		}
	}
}

// A run in English titles and words its checks in English and says so.
func TestRunLang(t *testing.T) {
	reg := NewRegistry()
	reg.Register(VPNConflict{})
	r := &Runner{Registry: reg, Timeout: time.Second}
	rep := r.Run(WithLang(context.Background(), "en"), platformOnly("plan9"))
	if rep.Lang != "en" || rep.Checks[0].Title != enTexts.titleVPNConflict || rep.Checks[0].Summary != "Not available on plan9" {
		t.Errorf("en run: %+v", rep)
	}
	rep = r.Run(context.Background(), platformOnly("plan9"))
	if rep.Lang != DefaultLang || rep.Checks[0].Title != zhTexts.titleVPNConflict {
		t.Errorf("default run: %+v", rep)
	}
	if LangOf(WithLang(context.Background(), "xx")) != DefaultLang {
		t.Error("unknown lang")
	}
}

// platformOnly is an Env that only knows its platform; checks for other
// platforms never call the rest.
func platformOnly(p string) Env { return onlyPlatform{p: p} }

type onlyPlatform struct {
	Env
	p string
}

func (o onlyPlatform) Platform() string { return o.p }

// Every UI language has health texts: adding a language to core/i18n
// without them fails here.
func TestTextsForEveryUILanguage(t *testing.T) {
	for _, l := range i18n.Langs() {
		if textTables[l.Tag] == nil {
			t.Errorf("no health texts for %s", l.Tag)
		}
	}
}

// The Chinese checks use the UI's terms (docs/i18n-glossary.md): 出口节点,
// 密钥, 后台助手, never the English words (R-unslop-24).
func TestChineseUsesGlossaryTerms(t *testing.T) {
	v := reflect.ValueOf(zhTexts)
	for i := range v.NumField() {
		s := v.Field(i).String()
		for _, w := range []string{"exit node", "secret", "helper", "panic"} {
			if strings.Contains(strings.ToLower(s), w) {
				t.Errorf("%s: %q has %q", v.Type().Field(i).Name, s, w)
			}
		}
	}
}

// Titles name the subject, not the passing condition (a failing "No
// conflicting VPN" reads as a pass).
func TestTitlesNameTheSubject(t *testing.T) {
	for tag, tab := range textTables {
		v := reflect.ValueOf(*tab)
		for i := range v.NumField() {
			name := v.Type().Field(i).Name
			if !strings.HasPrefix(name, "title") || name == "titleReport" {
				continue
			}
			s := v.Field(i).String()
			if strings.HasPrefix(s, "No ") || strings.HasPrefix(s, "没有") {
				t.Errorf("%s: %s = %q", tag, name, s)
			}
		}
	}
}
