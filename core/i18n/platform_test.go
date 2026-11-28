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
	before := written["en"].MsgTexts["refuses-exposed"]
	beforeSetup := written["en"].HistReasons["setup"]
	w := windowsLangs(t)
	if written["en"].MsgTexts["refuses-exposed"] != before || written["en"].HistReasons["setup"] != beforeSetup {
		t.Fatal("the Windows wording changed the tables as written")
	}
	if w[1].S.ShowInFinder != "Show in File Explorer" || !strings.Contains(w[1].S.MsgTexts["refuses-exposed"], "this PC") {
		t.Fatal("the Windows table is not worded")
	}
}

// The tables the running code reads are this system's wording of the
// tables as written (applyDesktop), and WordedFor gives the same for
// every system whatever system runs it.
func TestLiveTablesAreThisSystems(t *testing.T) {
	for _, l := range Langs() {
		if !reflect.DeepEqual(*l.S, *WordedFor(l.Tag, runtime.GOOS)) {
			t.Errorf("%s: the live table is not WordedFor(%s)", l.Tag, runtime.GOOS)
		}
		if !reflect.DeepEqual(*WordedFor(l.Tag, Darwin), written[l.Tag]) {
			t.Errorf("%s: the macOS table is not the table as written", l.Tag)
		}
	}
	if WordedFor("xx", Linux) != nil {
		t.Error("unknown tag worded")
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

// 6.4: a service's rows use the system's own words: launchd's label and
// definition (plist) on macOS, a service name and its registry key on
// Windows, a systemd unit and unit file on Linux.
func TestServiceRowsWorded(t *testing.T) {
	want := map[string][2]string{
		Darwin + "/zh-Hans": {"标签", "定义文件"}, Darwin + "/en": {"Label", "Definition"},
		Windows + "/zh-Hans": {"服务名", "注册表项"}, Windows + "/en": {"Service name", "Registry key"},
		Linux + "/zh-Hans": {"单元", "单元文件"}, Linux + "/en": {"Unit", "Unit file"},
	}
	for k, w := range want {
		goos, tag, _ := strings.Cut(k, "/")
		s := WordedFor(tag, goos)
		if s.ServiceLabel != w[0] || s.ServiceDefinition != w[1] {
			t.Errorf("%s: %q / %q, want %q / %q", k, s.ServiceLabel, s.ServiceDefinition, w[0], w[1])
		}
	}
}
