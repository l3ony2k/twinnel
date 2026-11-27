package i18n

import (
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// macWords are words a Windows user should not meet.
var macWords = regexp.MustCompile(`\bMac\b|macOS|Finder|访达|钥匙串|[Kk]eychain|LaunchDaemon|launchd|程序坞|⌘|⌥|⌃|菜单栏|[Mm]enu bar|管理员密码|administrator password|系统设置|System Settings|废纸篓|Trash|\.app\b`)

// windowsHidden are fields Windows never shows (the Dock and menu bar
// rows are macOS only, a Mac or Homebrew
// Tailscale is never detected there, LaunchAgents do not exist).
var windowsHidden = map[string]bool{
	".ShowInDock": true, ".DockWhenOpen": true, ".DockAlways": true, ".DockNever": true, ".DockOpen": true,
	".TraySpeedSwitch": true, ".NotificationsUnsupportedTip": true,
	".InstallKinds[mac-standalone]": true, ".InstallKinds[mac-appstore]": true,
	".TSUpdateHints[brew]": true, ".InvLaunchAgent": true, ".ItemLaunchAgent": true,
}

func TestWindowsWording(t *testing.T) {
	for _, l := range windowsLangs(t) {
		walkStrings(reflect.ValueOf(l.S).Elem(), "", func(path, s string) {
			if macWords.MatchString(s) && !windowsHidden[path[strings.LastIndex(path, "."):]] {
				t.Errorf("%s %s: %q", l.Tag, path, s)
			}
		})
	}
}

// windowsLangs are copies of the tables with the Windows wording (the
// real ones stay this host's).
func windowsLangs(t *testing.T) []Lang {
	var out []Lang
	for _, l := range Langs() {
		c := *l.S
		fn := windowsWording[l.Tag]
		if fn == nil {
			t.Fatalf("%s: no Windows wording", l.Tag)
		}
		fn(&c)
		out = append(out, Lang{Tag: l.Tag, S: &c})
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
