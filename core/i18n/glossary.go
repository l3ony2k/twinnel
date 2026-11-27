package i18n

import (
	"regexp"
	"slices"
	"strings"
)

// The platform glossary: words that name one system's things. A text a
// system shows must not name another system's things (a Windows user
// never reads "plist", a Mac user never reads "PowerShell"). Every string
// table is checked against it per platform: this package's Strings and
// wizard wording (glossary_test.go), core/health's texts and fix texts,
// core/healthfix's. Add a term here, not a deny-list in one test.

// Platform names, as runtime.GOOS spells them.
const (
	Darwin  = "darwin"
	Windows = "windows"
	Linux   = "linux"
)

// Platforms are the systems the tables are worded for.
var Platforms = []string{Darwin, Windows, Linux}

// term is one glossary entry: what matches it and the systems where it
// is the right word.
type term struct {
	name   string
	re     *regexp.Regexp
	native []string
}

func gt(name, re string, native ...string) term {
	return term{name, regexp.MustCompile(re), native}
}

var glossary = []term{
	// macOS.
	gt("plist", `(?i)\bplists?\b`, Darwin),
	gt("launchd", `\blaunch(d|ctl)\b`, Darwin),
	gt("LaunchDaemon", `Launch(Daemon|Agent)s?\b`, Darwin),
	gt("Keychain", `钥匙串|\b[Kk]eychain`, Darwin),
	gt("menu bar", `菜单栏|\b[Mm]enu bar\b`, Darwin),
	gt("Finder", `\bFinder\b|访达`, Darwin),
	gt("Dock", `程序坞|\bDock\b`, Darwin),
	gt(".app", `\w\.app\b`, Darwin),
	gt("Mac", `\bMac\b|\bmacOS\b`, Darwin),
	gt("Trash", `废纸篓|\bTrash\b`, Darwin),
	gt("System Settings", `系统设置|System Settings`, Darwin),
	gt("Login Items", `登录项|\b[Ll]ogin [Ii]tems?\b`, Darwin),
	gt("administrator password", `管理员密码|[Aa]dministrator password`, Darwin),
	gt("networksetup", `\b(networksetup|scutil)\b`, Darwin),
	gt("⌘", `[⌘⌥⌃]`, Darwin),
	// Unix (macOS and Linux): Windows says administrator, or SYSTEM for
	// the service account, and has no sudo.
	gt("root", `\broot\b`, Darwin, Linux),
	gt("sudo", `\bsudo\b`, Darwin, Linux),

	// Windows.
	gt("registry", `注册表|\b[Rr]egistry\b`, Windows),
	gt("service manager", `服务管理器|services\.msc|[Ss]ervice [Cc]ontrol [Mm]anager`, Windows),
	gt("UAC", `\bUAC\b|用户账户控制|User Account Control`, Windows),
	gt("taskbar", `任务栏|\b[Tt]askbar\b|通知区域|notification area`, Windows),
	gt("PowerShell", `PowerShell`, Windows),
	gt(".exe", `\w\.exe\b`, Windows),
	gt("File Explorer", `文件资源管理器|File Explorer`, Windows),
	gt("Task Scheduler", `任务计划程序|Task Scheduler|schtasks`, Windows),
	gt("this PC", `\b[Tt]his PC\b`, Windows),

	// Linux.
	gt("systemd", `\bsystemd\b|\bsystemctl\b|journalctl`, Linux),
	gt("pkexec", `\bpkexec\b|\bpolkit\b`, Linux),
	gt("NetworkManager", `NetworkManager|\bnmcli\b`, Linux),
}

// ForeignTerms returns the glossary terms in s that are not goos's words:
// none when s fits goos.
func ForeignTerms(goos, s string) []string {
	var out []string
	for _, g := range glossary {
		if !slices.Contains(g.native, goos) && g.re.MatchString(s) {
			out = append(out, g.name)
		}
	}
	return out
}

// ShownOn reports whether the field at path (".Field", ".Group.Field" or
// ".Map[key]", as the tests walk a table) is shown on goos. Fields not in
// shownOnly are shown everywhere.
func ShownOn(path, goos string) bool {
	if i := strings.LastIndex(path, "."); i >= 0 {
		path = path[i+1:]
	}
	only, ok := shownOnly[path]
	return !ok || slices.Contains(only, goos)
}

// shownOnly are the fields only some systems show (where the app reads
// them decides: a macOS-only row, a Unix-only check, a launchctl or
// systemctl plan step). A field that names a system's thing and is shown
// everywhere needs each system's wording instead (desktop.go,
// platform.go), or neutral words.
var shownOnly = map[string][]string{
	// The Dock, the menu bar's speed text, Login Items, LaunchAgents and
	// the Mac install kinds of Tailscale: macOS only.
	"ShowInDock": {Darwin}, "DockWhenOpen": {Darwin}, "DockAlways": {Darwin}, "DockNever": {Darwin}, "DockOpen": {Darwin},
	"TraySpeedSwitch": {Darwin}, "InvLaunchAgent": {Darwin}, "ItemLaunchAgent": {Darwin},
	"InstallKinds[mac-standalone]": {Darwin}, "InstallKinds[mac-appstore]": {Darwin}, "TSUpdateHints[brew]": {Darwin, Linux},
	"AsKinds[launch-agent]": {Darwin}, "AsKinds[launch-daemon]": {Darwin}, "AsKinds[login-item]": {Darwin},
	"AsKinds[scheduled-task]": {Windows}, "AsKinds[run-key]": {Windows},
	"AsKinds[systemd-system]": {Linux}, "AsKinds[systemd-user]": {Linux}, "AsKinds[xdg-autostart]": {Linux},
	// A link to the system's startup settings: none on Linux.
	"AsLoginItems": {Darwin, Windows}, "OpenAtLoginApprove": {Darwin, Windows},
	// Notifications need a packaged app on macOS and a notification
	// service on Linux; Windows always has one.
	"NotificationsUnsupportedTip": {Darwin, Linux},
	// Unix file ownership: Windows never runs the check.
	"WarningTexts[binary-writable]": {Darwin, Linux}, "TkEffReplaceableTip": {Darwin, Linux},
	// launchctl / systemctl plan steps (Windows plans are sc.exe steps,
	// PlatformStrings).
	"PlanBootstrap": {Darwin, Linux}, "PlanDaemonReload": {Linux},
	"PlanSCCreate": {Windows}, "PlanSCConfig": {Windows}, "PlanSCFailure": {Windows},
	"PlanSCDescription": {Windows}, "PlanSCStart": {Windows}, "PlanSCDelete": {Windows},
	// Tailscale login page: why Tailscale is down, per system.
	"TSLDownBodyMac": {Darwin}, "TSLDownBodyApp": {Darwin}, "TSLDownBodyWin": {Windows}, "TSLDownBodyLinux": {Linux},
}
