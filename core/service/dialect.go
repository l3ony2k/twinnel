package service

import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync/atomic"
)

// Dialect is how plans read on one system: each step as the command a
// person there would type, and the system's words for the account plans
// run as and for the app. Every printed plan, step and plan-related
// sentence goes through the running system's dialect (Current), so a
// Windows plan never says `rm -f` or "as root".
//
// Unix (macOS, Linux) is defined here; Windows' is in
// platform/windows/scm, which makes it Current on Windows (SetDialect).
type Dialect struct {
	// Command renders a step as the command it stands for, without notes.
	Command func(Step) string
	// Note renders a note after a command ("  # errors ignored").
	Note func(string) string
	// Admin is the account plans run as: "root", "administrator".
	Admin string
	// App names the app as people see it: "Twinnel.app", "Twinnel".
	App string
}

// Step renders a step with its notes (errors ignored, retried).
func (d Dialect) Step(s Step) string {
	out := d.Command(s)
	if s.Kind == StepRun && s.Retry > 0 {
		out += d.Note(fmt.Sprintf("retried up to %d times while busy", s.Retry))
	}
	if s.IgnoreError {
		out += d.Note("errors ignored")
	}
	if s.Always {
		out += d.Note("also when a step before failed")
	}
	return out
}

// FormatPlan renders a plan for a dry run: each step on a line, file
// contents of write steps included.
func (d Dialect) FormatPlan(w io.Writer, steps []Step) {
	for i, s := range steps {
		fmt.Fprintf(w, "%d. %s\n", i+1, d.Step(s))
		if s.Kind == StepWrite {
			fmt.Fprintf(w, "----- %s -----\n%s", s.Path, s.Content)
			if len(s.Content) > 0 && s.Content[len(s.Content)-1] != '\n' {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "----- end -----\n")
		}
	}
}

// AsAdmin is "as root" / "as administrator".
func (d Dialect) AsAdmin() string { return "as " + d.Admin }

// Unix is the dialect of macOS and Linux: POSIX shell commands.
var Unix = Dialect{
	Command: unixCommand,
	Note:    func(n string) string { return "  # " + n },
	Admin:   "root",
	App:     unixApp(runtime.GOOS),
}

func unixApp(goos string) string {
	if goos == "darwin" {
		return "Twinnel.app"
	}
	return "Twinnel"
}

var current atomic.Pointer[Dialect]

// Current is the dialect of the system this program runs on.
func Current() Dialect {
	if d := current.Load(); d != nil {
		return *d
	}
	return Unix
}

// SetDialect makes d Current (platform code, at init).
func SetDialect(d Dialect) { current.Store(&d) }

// String renders the step in the running system's dialect.
func (s Step) String() string { return Current().Step(s) }

// FormatPlan renders a plan in the running system's dialect.
func FormatPlan(w io.Writer, steps []Step) { Current().FormatPlan(w, steps) }

func unixCommand(s Step) string {
	switch s.Kind {
	case StepMkdir:
		return fmt.Sprintf("install -d -m %04o %s", s.Mode.Perm(), shellQuote(s.Path))
	case StepCopy:
		return fmt.Sprintf("install -m %04o %s %s", s.Mode.Perm(), shellQuote(s.Src), shellQuote(s.Path))
	case StepWrite:
		return fmt.Sprintf("write %s (mode %04o, %d bytes)", shellQuote(s.Path), s.Mode.Perm(), len(s.Content))
	case StepRemove:
		return "rm -f " + shellQuote(s.Path)
	case StepRemoveAll:
		return "rm -rf " + shellQuote(s.Path)
	case StepRename:
		return "mv -f " + shellQuote(s.Src) + " " + shellQuote(s.Path)
	case StepRun:
		return shellJoin(s.Argv)
	case StepWait:
		return WaitText(s, shellJoin(s.Argv))
	}
	return string(s.Kind)
}

// WaitText is a wait step in words around its command (cmd, already
// quoted the dialect's way).
func WaitText(s Step, cmd string) string {
	until := fmt.Sprintf("%q", s.Until)
	for _, o := range s.OrUntil {
		until += fmt.Sprintf(" or %q", o)
	}
	return fmt.Sprintf("wait until `%s` says %s (up to %s)", cmd, until, s.Timeout)
}

func shellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shellQuote(a)
	}
	return strings.Join(q, " ")
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=@%+,", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// StepView is a step as data, for JSON (the app reads plans this way
// instead of parsing the text): what it does, on what, and how the
// running system's dialect prints it.
type StepView struct {
	Kind        StepKind `json:"kind"`
	Text        string   `json:"text"`
	Path        string   `json:"path,omitempty"`
	Src         string   `json:"src,omitempty"`
	Argv        []string `json:"argv,omitempty"`
	Content     string   `json:"content,omitempty"` // write
	IgnoreError bool     `json:"ignore_error,omitempty"`
}

// Views describes steps in dialect d.
func (d Dialect) Views(steps []Step) []StepView {
	out := make([]StepView, len(steps))
	for i, s := range steps {
		out[i] = StepView{Kind: s.Kind, Text: d.Step(s), Path: s.Path, Src: s.Src, Argv: s.Argv, IgnoreError: s.IgnoreError}
		if s.Kind == StepWrite {
			out[i].Content = string(s.Content)
		}
	}
	return out
}
