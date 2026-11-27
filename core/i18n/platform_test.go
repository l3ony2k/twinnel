package i18n

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// macWords are words a Windows user should not meet.
var macWords = regexp.MustCompile(`\bMac\b|macOS|Finder|访达|钥匙串|[Kk]eychain|LaunchDaemon|launchd|程序坞|⌘|⌥|⌃|菜单栏|[Mm]enu bar|管理员密码|administrator password|系统设置|System Settings|废纸篓|Trash|\.app\b`)

// windowsHidden are fields Windows never shows (the Dock and menu bar
// rows are macOS only, the CLI link row is hidden, a Mac or Homebrew
// Tailscale is never detected there, LaunchAgents do not exist).
var windowsHidden = map[string]bool{
	".ShowInDock": true, ".DockWhenOpen": true, ".DockAlways": true, ".DockNever": true, ".DockOpen": true,
	".TraySpeedSwitch": true, ".NotificationsUnsupportedTip": true, ".TipAgentCLI": true,
	".InstallKinds[mac-standalone]": true, ".InstallKinds[mac-appstore]": true,
	".TSUpdateHints[brew]": true, ".InvLaunchAgent": true, ".ItemLaunchAgent": true,
}

func TestWindowsWording(t *testing.T) {
	for _, l := range platformLangs("windows", Langs()) {
		walkStrings(reflect.ValueOf(l.S).Elem(), "", func(path, s string) {
			if macWords.MatchString(s) && !windowsHidden[path[strings.LastIndex(path, "."):]] {
				t.Errorf("%s %s: %q", l.Tag, path, s)
			}
		})
	}
}

// The patch works on copies: macOS keeps its words, and the shared maps
// are not changed under it.
func TestWindowsPatchCopies(t *testing.T) {
	win := platformLangs("windows", Langs())
	if win[1].S == &en || en.ShowInFinder != "Show in Finder" || !strings.Contains(en.MsgTexts["refuses-exposed"], "this Mac") {
		t.Fatal("the Windows patch changed the macOS table")
	}
	if win[1].S.ShowInFinder != "Show in File Explorer" || !strings.Contains(win[1].S.MsgTexts["refuses-exposed"], "this PC") {
		t.Fatal("the Windows table is not patched")
	}
	if got := platformLangs("darwin", Langs()); got[0].S != &zh {
		t.Fatal("macOS should keep the tables as written")
	}
}

// The Windows Chinese table keeps to the same rule as the macOS one.
func TestWindowsChineseHasNoEnglish(t *testing.T) {
	zh := platformLangs("windows", Langs())[0].S
	walkStrings(reflect.ValueOf(*zh), "zh", func(path, s string) {
		if w := EnglishWords(s); len(w) > 0 && !pendingElsewhere[path] {
			t.Errorf("%s: %s ← %s", path, strings.Join(w, " "), s)
		}
	})
}
