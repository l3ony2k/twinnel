// Package autostart finds other proxy clients that will start again by
// themselves (at login, at boot, on a schedule) and plans how to turn that
// off and back on.
//
// A takeover stops a competing client (Clash Party, Clash Verge, ...) that
// is running; if it also left an autostart entry behind (a Windows
// scheduled task or Run key, a macOS LaunchAgent or login item, a Linux
// systemd unit or XDG autostart file) it comes back at the next login and
// takes the default route and port 9090 again. "Running" is not enough:
// this package reads what is registered to start.
//
// Reading is platform code (a Source); this package matches entries to
// known clients and turns a disable or enable into commands (Plan) the
// helper runs, recorded and tested like its service steps. A disable is
// always reversible: tasks and units are disabled, not deleted; a Run key
// is switched off the way Task Manager does (StartupApproved), its value
// left in place. Entries the helper cannot change (a macOS login item, a
// Linux user unit) get a guided or manual path instead.
package autostart

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Kind is where an entry is registered.
type Kind string

const (
	ScheduledTask Kind = "scheduled-task" // Windows Task Scheduler
	RunKey        Kind = "run-key"        // Windows ...\CurrentVersion\Run
	LaunchAgent   Kind = "launch-agent"   // macOS, per user
	LaunchDaemon  Kind = "launch-daemon"  // macOS, system
	LoginItem     Kind = "login-item"     // macOS, an app's own login item
	SystemdSystem Kind = "systemd-system" // Linux system unit
	SystemdUser   Kind = "systemd-user"   // Linux user unit
	XDGAutostart  Kind = "xdg-autostart"  // Linux ~/.config/autostart/*.desktop
)

// How an entry is turned off.
const (
	ByHelper = "helper" // one click: the helper runs Plan's commands
	Guided   = "guided" // the system's own settings (Entry.Open)
	Manual   = "manual" // a command the user runs (Entry.Command)
)

// Entry is one registered autostart of a competing client.
type Entry struct {
	// ID is stable across reads: kind and name (and the user, for
	// per-user entries).
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	// Name is the task path, Run value name, launchd label, unit or
	// .desktop file name.
	Name string `json:"name"`
	// Client is the competing client it starts ("Clash Party").
	Client string `json:"client"`
	// Program is what it runs (the executable path, no arguments).
	Program string `json:"program,omitempty"`
	// Location is where it is registered: the task file, the registry
	// key, the plist or unit file.
	Location string `json:"location,omitempty"`
	// User owns a per-user entry (a SID on Windows, a uid or user name
	// elsewhere); empty for system-wide ones.
	User    string `json:"user,omitempty"`
	Enabled bool   `json:"enabled"`
	// Disable is how it is turned off: ByHelper, Guided or Manual.
	Disable string `json:"disable"`
	// Command is the command a Manual entry is turned off with.
	Command string `json:"command,omitempty"`
	// Open is the settings page a Guided entry is turned off in.
	Open string `json:"open,omitempty"`
}

// Source reads the registered autostarts of one platform (all of them;
// Find picks the competing clients).
type Source interface {
	List(ctx context.Context) ([]Entry, error)
}

// Client patterns: a lowercase substring of the name or program, and the
// client it means. Order matters (the first match wins).
var clients = []struct{ pat, name string }{
	{"mihomo-party", "Clash Party"},
	{"clash party", "Clash Party"},
	{"clash-party", "Clash Party"},
	{"clashparty", "Clash Party"},
	{"clash-verge", "Clash Verge"},
	{"clash verge", "Clash Verge"},
	{"clash_verge", "Clash Verge"},
	{"io.github.clash-verge-rev", "Clash Verge"},
	{"clash for windows", "Clash for Windows"},
	{"clash-for-windows", "Clash for Windows"},
	{"clashx", "ClashX"},
	{"flclash", "FlClash"},
	{"clash-nyanpasu", "Clash Nyanpasu"},
	{"nyanpasu", "Clash Nyanpasu"},
	{"sing-box", "sing-box"},
	{"v2rayn", "v2rayN"},
	{"nekoray", "NekoRay"},
	{"nekobox", "NekoBox"},
	{"hiddify", "Hiddify"},
	{"mihomo", "mihomo"},
	{"clash-meta", "mihomo"},
}

// Match returns the competing client an autostart with this name and
// program starts, if any. Twinnel's own entries never match.
func Match(name, program string) (string, bool) {
	s := strings.ToLower(name + "\x00" + program)
	if strings.Contains(s, "twinnel") {
		return "", false
	}
	for _, c := range clients {
		if strings.Contains(s, c.pat) {
			return c.name, true
		}
	}
	return "", false
}

// Find lists the competing clients' entries of src, without the ones
// Twinnel runs itself (skip: launchd labels, unit or service names it
// manages or adopted, e.g. the mihomo service of a takeover).
func Find(ctx context.Context, src Source, skip ...string) ([]Entry, error) {
	if src == nil {
		return nil, ErrNotSupported
	}
	all, err := src.List(ctx)
	out := []Entry{}
	for _, e := range all {
		if skipped(e, skip) {
			continue
		}
		client, ok := Match(e.Name, e.Program)
		if !ok {
			continue
		}
		e.Client = client
		if e.ID == "" {
			e.ID = MakeID(e.Kind, e.User, e.Name)
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Enabled != out[j].Enabled {
			return out[i].Enabled
		}
		return out[i].ID < out[j].ID
	})
	return out, err
}

func skipped(e Entry, skip []string) bool {
	for _, s := range skip {
		if s != "" && strings.EqualFold(strings.TrimSuffix(e.Name, ".service"), strings.TrimSuffix(s, ".service")) {
			return true
		}
	}
	return false
}

// MakeID is an entry's ID.
func MakeID(k Kind, user, name string) string {
	if user != "" {
		return string(k) + ":" + user + ":" + name
	}
	return string(k) + ":" + name
}

// ErrNotSupported: no Source on this platform.
var ErrNotSupported = errors.New("reading autostart entries is not supported on this platform")

// Op is a change to an entry.
type Op string

const (
	OpDisable Op = "disable"
	OpEnable  Op = "enable" // the undo of a disable
)

// Plan returns the commands that apply op to e (argv lists, run by the
// helper as root / LocalSystem). An entry the helper cannot change
// returns an error naming the way to do it.
func Plan(e Entry, op Op) ([][]string, error) {
	if e.Disable != ByHelper {
		switch e.Disable {
		case Guided:
			return nil, fmt.Errorf("%s is turned off in the system settings (%s)", e.Name, e.Open)
		case Manual:
			return nil, fmt.Errorf("%s is turned off with: %s", e.Name, e.Command)
		}
		return nil, fmt.Errorf("%s cannot be changed by Twinnel", e.Name)
	}
	if op != OpDisable && op != OpEnable {
		return nil, fmt.Errorf("unknown op %q", op)
	}
	on := op == OpEnable
	switch e.Kind {
	case ScheduledTask:
		flag := "/Disable"
		if on {
			flag = "/Enable"
		}
		return [][]string{{SystemTool("schtasks.exe"), "/Change", "/TN", e.Name, flag}}, nil
	case RunKey:
		// The value stays; StartupApproved says whether it runs (what
		// Task Manager's Startup tab writes): 02... on, 03... off.
		data := "030000000000000000000000"
		if on {
			data = "020000000000000000000000"
		}
		key := approvedKey(e.Location)
		if key == "" {
			return nil, fmt.Errorf("%s: unknown Run key %q", e.Name, e.Location)
		}
		return [][]string{{SystemTool("reg.exe"), "add", key, "/v", e.Name, "/t", "REG_BINARY", "/d", data, "/f"}}, nil
	case LaunchAgent:
		if e.User == "" {
			return nil, fmt.Errorf("%s: no user for a LaunchAgent", e.Name)
		}
		verb := "disable"
		if on {
			verb = "enable"
		}
		// disable persists across logins; bootout stops a loaded agent.
		cmds := [][]string{{"/bin/launchctl", verb, "gui/" + e.User + "/" + e.Name}}
		if !on {
			cmds = append(cmds, []string{"/bin/launchctl", "bootout", "gui/" + e.User + "/" + e.Name})
		}
		return cmds, nil
	case LaunchDaemon:
		verb := "disable"
		if on {
			verb = "enable"
		}
		cmds := [][]string{{"/bin/launchctl", verb, "system/" + e.Name}}
		if !on {
			cmds = append(cmds, []string{"/bin/launchctl", "bootout", "system/" + e.Name})
		}
		return cmds, nil
	case SystemdSystem:
		if on {
			return [][]string{{"systemctl", "enable", e.Name}}, nil
		}
		return [][]string{{"systemctl", "disable", "--now", e.Name}}, nil
	}
	return nil, fmt.Errorf("%s: %s entries are not changed by the helper", e.Name, e.Kind)
}

// approvedKey is the StartupApproved key of a Run key ("" if it is not
// one): <hive>\Software\Microsoft\Windows\CurrentVersion\Run →
// <hive>\Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run,
// and the 32-bit view (WOW6432Node) → ...\StartupApproved\Run32.
func approvedKey(runKey string) string {
	const (
		run   = `\software\microsoft\windows\currentversion\run`
		run32 = `\software\wow6432node\microsoft\windows\currentversion\run`
		dst   = `\Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	)
	l := strings.ToLower(runKey)
	switch {
	case strings.HasSuffix(l, run32):
		return runKey[:len(runKey)-len(run32)] + dst + "32"
	case strings.HasSuffix(l, run):
		return runKey[:len(runKey)-len(run)] + dst
	}
	return ""
}

// SystemTool is a program in %SystemRoot%\System32 (never one on PATH).
// A variable for tests.
var SystemTool = func(name string) string {
	root := os.Getenv("SystemRoot")
	if len(root) < 3 || root[1] != ':' {
		root = `C:\Windows`
	}
	return strings.TrimRight(root, `\`) + `\System32\` + name
}

// Undo is what turns a disable back: the same entries, enabled.
type Undo struct {
	Entries []Entry `json:"entries"`
}
