package linux

import (
	"errors"
	"strings"
)

// terminals are the terminal emulators TerminalArgv tries, in order, with
// the words that make each run a command: the freedesktop launcher
// (xdg-terminal-exec) first, then the defaults of Hyprland setups, KDE,
// GNOME and others.
var terminals = []struct {
	name string
	pre  []string
}{
	{"xdg-terminal-exec", nil},
	{"kitty", nil},
	{"foot", nil},
	{"ghostty", []string{"-e"}},
	{"alacritty", []string{"-e"}},
	{"wezterm", []string{"start", "--"}},
	{"konsole", []string{"-e"}},
	{"kgx", []string{"--"}},
	{"gnome-terminal", []string{"--"}},
	{"xfce4-terminal", []string{"-x"}},
	{"xterm", []string{"-e"}},
}

// ErrNoTerminal: no terminal emulator found to run a command in.
var ErrNoTerminal = errors.New("no terminal emulator found")

// TerminalArgv is the command that opens a terminal running argv: $TERMINAL
// when it names a program (run with -e, the convention most follow), else
// the first of terminals on $PATH. lookPath is exec.LookPath; getenv is
// os.Getenv. argv's words stay argv words: no shell.
func TerminalArgv(argv []string, getenv func(string) string, lookPath func(string) (string, error)) ([]string, error) {
	if t := strings.TrimSpace(getenv("TERMINAL")); t != "" && !strings.ContainsAny(t, " \t") {
		if p, err := lookPath(t); err == nil {
			return append([]string{p, "-e"}, argv...), nil
		}
	}
	for _, t := range terminals {
		if p, err := lookPath(t.name); err == nil {
			out := append([]string{p}, t.pre...)
			return append(out, argv...), nil
		}
	}
	return nil, ErrNoTerminal
}

// HelperJournalArgv shows the helper unit's journal, newest at the end:
// what the helper printed to stdout/stderr (a crash), next to its own log
// in <state>/logs/helper.log.
func HelperJournalArgv() []string {
	return []string{"journalctl", "--unit", HelperUnit + ".service", "--pager-end"}
}
