package i18n

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// windowsLangs are copies of the tables with the Windows wording (the
// real ones stay this host's).
func windowsLangs(t *testing.T) []Lang {
	var out []Lang
	for _, l := range Langs() {
		out = append(out, Lang{Tag: l.Tag, S: wordedFor(t, l, Windows)})
	}
	return out
}

// The wording replaces the shared maps instead of changing them.
func TestWindowsWordingCopiesMaps(t *testing.T) {
	before := en.MsgTexts["refuses-exposed"]
	w := windowsLangs(t)
	if en.MsgTexts["refuses-exposed"] != before || runtime.GOOS != "windows" && en.HistReasons["setup"] == w[1].S.HistReasons["setup"] {
		t.Fatal("the Windows wording changed a shared map")
	}
	if w[1].S.ShowInFinder != "Show in File Explorer" || !strings.Contains(w[1].S.MsgTexts["refuses-exposed"], "this PC") {
		t.Fatal("the Windows table is not worded")
	}
}

// The Windows Chinese table keeps to the same rule as the macOS one.
func TestWindowsChineseHasNoEnglish(t *testing.T) {
	zh := windowsLangs(t)[0].S
	walkStrings(reflect.ValueOf(*zh), "zh", func(path, s string) {
		if w := EnglishWords(s); len(w) > 0 && !pendingElsewhere[path] {
			t.Errorf("%s: %s ← %s", path, strings.Join(w, " "), s)
		}
	})
}
