package autostart

// Readers over files, shared by the platforms (the helper points them at
// the real directories; tests at temp dirs). The Windows registry part is
// in platform/windows.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// ---- macOS -----------------------------------------------------------------

// LaunchdDir is a directory of launchd plists.
type LaunchdDir struct {
	Dir  string
	Kind Kind   // LaunchAgent or LaunchDaemon
	User string // uid of a per-user LaunchAgents dir ("" for /Library/LaunchAgents: every user)
}

// LaunchdSource reads launchd plists and the apps' own login items.
type LaunchdSource struct {
	Dirs []LaunchdDir
	// Apps are .app bundles' parent dirs (/Applications, ~/Applications):
	// a client that registers itself as a login item ships its helper in
	// Contents/Library/LoginItems.
	Apps []string
	// Users are the uids a /Library/LaunchAgents agent is disabled for
	// (the console user).
	Users []string
	// JSON converts a plist to JSON (`plutil -convert json -o - <path>`).
	JSON func(ctx context.Context, path string) ([]byte, error)
	// Disabled lists the labels launchd has disabled in a domain
	// ("system", "gui/501"): `launchctl print-disabled <domain>`.
	Disabled func(ctx context.Context, domain string) (map[string]bool, error)
}

// LoginItemsSettings opens System Settings → General → Login Items.
const LoginItemsSettings = "x-apple.systempreferences:com.apple.LoginItems-Settings.extension"

func (s *LaunchdSource) List(ctx context.Context) ([]Entry, error) {
	var out []Entry
	var errs []error
	disabled := map[string]map[string]bool{}
	isDisabled := func(domain, label string) bool {
		if s.Disabled == nil {
			return false
		}
		m, ok := disabled[domain]
		if !ok {
			m, _ = s.Disabled(ctx, domain)
			disabled[domain] = m
		}
		return m[label]
	}
	for _, d := range s.Dirs {
		plists, _ := filepath.Glob(filepath.Join(d.Dir, "*.plist"))
		for _, pl := range plists {
			b, err := os.ReadFile(pl)
			if err != nil {
				if errors.Is(err, fs.ErrPermission) {
					errs = append(errs, err)
				}
				continue
			}
			// Cheap filter before converting: only plists that name a client.
			if _, ok := Match(string(b), ""); !ok {
				continue
			}
			if s.JSON == nil {
				continue
			}
			js, err := s.JSON(ctx, pl)
			if err != nil {
				continue
			}
			var v struct {
				Label            string
				Program          string
				ProgramArguments []string
				Disabled         bool
				RunAtLoad        bool
				KeepAlive        any
			}
			if json.Unmarshal(js, &v) != nil || v.Label == "" {
				continue
			}
			prog := v.Program
			if prog == "" && len(v.ProgramArguments) > 0 {
				prog = v.ProgramArguments[0]
			}
			users := []string{d.User}
			if d.Kind == LaunchAgent && d.User == "" && len(s.Users) > 0 {
				users = s.Users // a system-wide agent runs for every user
			}
			for _, u := range users {
				e := Entry{Kind: d.Kind, Name: v.Label, Program: prog, Location: pl, User: u, Disable: ByHelper}
				domain := "system"
				if d.Kind == LaunchAgent {
					domain = "gui/" + u
				}
				e.Enabled = !v.Disabled && !isDisabled(domain, v.Label)
				if d.Kind == LaunchAgent && u == "" {
					e.Disable, e.Open = Guided, LoginItemsSettings
				}
				out = append(out, e)
			}
		}
	}
	for _, apps := range s.Apps {
		bundles, _ := filepath.Glob(filepath.Join(apps, "*.app"))
		for _, app := range bundles {
			name := strings.TrimSuffix(filepath.Base(app), ".app")
			if _, ok := Match(name, ""); !ok {
				continue
			}
			items, _ := filepath.Glob(filepath.Join(app, "Contents", "Library", "LoginItems", "*.app"))
			for _, it := range items {
				out = append(out, Entry{Kind: LoginItem, Name: name, Program: it, Location: app,
					Enabled: true, Disable: Guided, Open: LoginItemsSettings})
			}
		}
	}
	return out, errors.Join(errs...)
}

// ParsePrintDisabled reads `launchctl print-disabled <domain>`:
//
//	disabled services = {
//		"com.example.agent" => disabled
//		"com.example.other" => enabled
//	}
//
// (older systems print true/false).
func ParsePrintDisabled(out []byte) map[string]bool {
	m := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		k, v, ok := strings.Cut(l, "=>")
		if !ok {
			continue
		}
		k = strings.Trim(strings.TrimSpace(k), `"`)
		v = strings.TrimSpace(v)
		m[k] = v == "disabled" || v == "true"
	}
	return m
}

// ---- Linux -----------------------------------------------------------------

// LinuxHome is a user's home whose autostarts are read.
type LinuxHome struct {
	Home string
	User string // user name
}

// LinuxSource reads systemd units and XDG autostart files.
type LinuxSource struct {
	// SystemDirs hold system units (/etc/systemd/system,
	// /usr/lib/systemd/system); a unit is enabled when a *.wants dir in
	// /etc/systemd/system links it.
	SystemDirs []string
	// WantsRoot is where the system *.wants dirs are (/etc/systemd/system).
	WantsRoot string
	// XDGDirs are system autostart dirs (/etc/xdg/autostart).
	XDGDirs []string
	Homes   []LinuxHome
}

func (s *LinuxSource) List(ctx context.Context) ([]Entry, error) {
	var out []Entry
	seen := map[string]bool{}
	for _, dir := range s.SystemDirs {
		for _, u := range units(dir) {
			if seen[u.name] {
				continue
			}
			seen[u.name] = true
			if _, ok := Match(u.name, u.exec); !ok {
				continue
			}
			out = append(out, Entry{Kind: SystemdSystem, Name: u.name, Program: u.exec, Location: u.path,
				Enabled: wanted(s.WantsRoot, u.name), Disable: ByHelper})
		}
	}
	for _, h := range s.Homes {
		udir := filepath.Join(h.Home, ".config", "systemd", "user")
		for _, u := range units(udir) {
			if _, ok := Match(u.name, u.exec); !ok {
				continue
			}
			out = append(out, Entry{Kind: SystemdUser, Name: u.name, Program: u.exec, Location: u.path, User: h.User,
				Enabled: wanted(udir, u.name), Disable: Manual, Command: "systemctl --user disable --now " + u.name})
		}
		out = append(out, desktopEntries(filepath.Join(h.Home, ".config", "autostart"), h.User)...)
	}
	for _, d := range s.XDGDirs {
		out = append(out, desktopEntries(d, "")...)
	}
	return out, nil
}

type unitFile struct{ name, path, exec string }

func units(dir string) []unitFile {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.service"))
	var out []unitFile
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		u := unitFile{name: filepath.Base(p), path: p}
		for _, l := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "ExecStart="); ok {
				v = strings.TrimLeft(v, "@-:+!")
				u.exec, _, _ = strings.Cut(strings.TrimSpace(v), " ")
				break
			}
		}
		out = append(out, u)
	}
	return out
}

// wanted: a *.wants dir under root links the unit.
func wanted(root, unit string) bool {
	if root == "" {
		return false
	}
	links, _ := filepath.Glob(filepath.Join(root, "*.wants", unit))
	return len(links) > 0
}

func desktopEntries(dir, user string) []Entry {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.desktop"))
	var out []Entry
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var exec string
		enabled := true
		for _, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSpace(l)
			switch {
			case strings.HasPrefix(l, "Exec="):
				exec, _, _ = strings.Cut(strings.TrimPrefix(l, "Exec="), " ")
			case strings.EqualFold(l, "Hidden=true"), strings.EqualFold(l, "X-GNOME-Autostart-enabled=false"):
				enabled = false
			}
		}
		name := filepath.Base(p)
		if _, ok := Match(name, exec); !ok {
			continue
		}
		e := Entry{Kind: XDGAutostart, Name: name, Program: exec, Location: p, User: user, Enabled: enabled, Disable: Manual}
		if user != "" {
			e.Command = "echo Hidden=true >> " + shellQuote(p)
		} else {
			e.Command = "mkdir -p ~/.config/autostart && printf '[Desktop Entry]\\nHidden=true\\n' > ~/.config/autostart/" + shellQuote(name)
		}
		out = append(out, e)
	}
	return out
}

func shellQuote(s string) string {
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ---- Windows scheduled tasks ----------------------------------------------

// TaskDirSource reads the Task Scheduler's task files (%SystemRoot%\
// System32\Tasks: one XML file per task, the folder tree is the task
// path). Only LocalSystem and Administrators can read them; the helper
// runs as LocalSystem.
type TaskDirSource struct {
	Dir string
}

func (s *TaskDirSource) List(ctx context.Context) ([]Entry, error) {
	var out []Entry
	var denied error
	err := filepath.WalkDir(s.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrPermission) && denied == nil {
				denied = err
			}
			if d != nil && d.IsDir() && p != s.Dir {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || ctx.Err() != nil {
			return ctx.Err()
		}
		rel, _ := filepath.Rel(s.Dir, p)
		name := `\` + strings.ReplaceAll(filepath.ToSlash(rel), "/", `\`)
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		t, err := ParseTask(b)
		if err != nil {
			return nil
		}
		out = append(out, Entry{Kind: ScheduledTask, Name: name, Program: t.Command, Location: p, Enabled: t.Enabled, Disable: ByHelper})
		return nil
	})
	if err == nil {
		err = denied
	}
	return out, err
}

// Task is what ParseTask reads from a task's XML.
type Task struct {
	Command string
	Enabled bool
}

// ParseTask reads a Task Scheduler XML file (UTF-16 with a BOM, as the
// Task Scheduler writes them, or UTF-8).
func ParseTask(b []byte) (Task, error) {
	b = toUTF8(b)
	// The declaration names UTF-16; the bytes are UTF-8 now.
	if i := bytes.Index(b, []byte("?>")); i >= 0 && bytes.HasPrefix(bytes.TrimSpace(b), []byte("<?xml")) {
		b = b[i+2:]
	}
	var v struct {
		Settings struct {
			Enabled *bool `xml:"Enabled"`
		} `xml:"Settings"`
		Actions struct {
			Exec []struct {
				Command string `xml:"Command"`
			} `xml:"Exec"`
		} `xml:"Actions"`
	}
	if err := xml.Unmarshal(b, &v); err != nil {
		return Task{}, err
	}
	t := Task{Enabled: v.Settings.Enabled == nil || *v.Settings.Enabled}
	if len(v.Actions.Exec) > 0 {
		t.Command = strings.Trim(strings.TrimSpace(v.Actions.Exec[0].Command), `"`)
	}
	return t, nil
}

func toUTF8(b []byte) []byte {
	if len(b) >= 2 && (b[0] == 0xff && b[1] == 0xfe || b[0] == 0xfe && b[1] == 0xff) {
		le := b[0] == 0xff
		b = b[2:]
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			if le {
				u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
			} else {
				u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
			}
		}
		return []byte(string(utf16.Decode(u)))
	}
	return bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
}

// Multi is several sources read as one.
type Multi []Source

func (m Multi) List(ctx context.Context) ([]Entry, error) {
	var out []Entry
	var errs []error
	for _, s := range m {
		if s == nil {
			continue
		}
		es, err := s.List(ctx)
		out = append(out, es...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return out, errors.Join(errs...)
}
