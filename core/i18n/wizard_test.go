package i18n

import (
	"reflect"
	"strings"
	"testing"
)

// Every language words the wizard for every system, with no field empty.
func TestWizardOSComplete(t *testing.T) {
	for _, l := range Langs() {
		m := wizardOS[l.Tag]
		for _, goos := range []string{"darwin", "windows", "linux"} {
			w, ok := m[goos]
			if !ok {
				t.Fatalf("%s: no %s wording", l.Tag, goos)
			}
			v := reflect.ValueOf(w)
			for i := 0; i < v.NumField(); i++ {
				if v.Field(i).String() == "" {
					t.Errorf("%s %s: %s is empty", l.Tag, goos, v.Type().Field(i).Name)
				}
			}
		}
	}
}

// Windows and Linux never meet a Mac word; Chinese has no stray English;
// English has no Chinese.
func TestWizardOSWording(t *testing.T) {
	for _, l := range Langs() {
		for goos, w := range wizardOS[l.Tag] {
			walkStrings(reflect.ValueOf(w), l.Tag+"."+goos, func(path, s string) {
				if goos != "darwin" && macWords.MatchString(s) {
					t.Errorf("%s: Mac words in %q", path, s)
				}
				if l.Tag == "zh-Hans" {
					if e := EnglishWords(s); len(e) > 0 {
						t.Errorf("%s: %s ← %s", path, strings.Join(e, " "), s)
					}
				} else if HasHan(s) {
					t.Errorf("%s: Chinese in %q", path, s)
				}
			})
		}
	}
}

// The progress bar and the plan line up with the wizard's six steps.
func TestWizardStepNames(t *testing.T) {
	for _, l := range Langs() {
		if n := len(l.S.WizStepNames); n != 6 {
			t.Errorf("%s: %d step names, want 6", l.Tag, n)
		}
		if n := len(l.S.WizPlan); n != 4 {
			t.Errorf("%s: %d plan lines, want 4 (one per step between 欢迎 and 完成)", l.Tag, n)
		}
	}
}

// WizardOSFor follows the current language and falls back to Linux's
// words on another system.
func TestWizardOSFor(t *testing.T) {
	defer Set(Tag())
	Set("en")
	if got := WizardOSFor("windows").Machine; got != "this PC" {
		t.Errorf("en windows: %q", got)
	}
	if got := WizardOSFor("freebsd").Machine; got != wizardOS["en"]["linux"].Machine {
		t.Errorf("en freebsd: %q", got)
	}
	Set("zh")
	if got := WizardOSFor("darwin").Machine; got != "这台 Mac" {
		t.Errorf("zh darwin: %q", got)
	}
}
