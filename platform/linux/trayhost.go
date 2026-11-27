package linux

import (
	"context"
	"strings"
)

// Tray icons on Linux are StatusNotifierItems: they show only while a
// StatusNotifierWatcher owns its name on the session bus (KDE Plasma,
// quickshell/waybar/ags bars with a tray, GNOME with the AppIndicator
// extension, Xfce's and others' tray plugins).

// StatusNotifierWatcher is the bus name the tray host owns.
const StatusNotifierWatcher = "org.kde.StatusNotifierWatcher"

// busQueries ask the session bus whether StatusNotifierWatcher has an
// owner, with the tools a desktop has: busctl (systemd), gdbus (GLib),
// dbus-send. Each prints a boolean its own way (parseNameHasOwner).
var busQueries = [][]string{
	{"busctl", "--user", "call", "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "NameHasOwner", "s", StatusNotifierWatcher},
	{"gdbus", "call", "--session", "--dest", "org.freedesktop.DBus", "--object-path", "/org/freedesktop/DBus", "--method", "org.freedesktop.DBus.NameHasOwner", StatusNotifierWatcher},
	{"dbus-send", "--session", "--print-reply", "--dest=org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus.NameHasOwner", "string:" + StatusNotifierWatcher},
}

// Runner runs a command and returns its stdout (exec.CommandContext in
// the app; a fake in tests).
type Runner func(ctx context.Context, argv []string) ([]byte, error)

// TrayHostPresent asks the session bus whether a tray host runs. known is
// false when no tool could ask (none installed, no session bus).
func TrayHostPresent(ctx context.Context, run Runner) (present, known bool) {
	for _, q := range busQueries {
		out, err := run(ctx, q)
		if err != nil {
			continue
		}
		if v, ok := parseNameHasOwner(string(out)); ok {
			return v, true
		}
	}
	return false, false
}

// parseNameHasOwner reads "b true" (busctl), "(true,)" (gdbus) or
// "boolean true" (dbus-send).
func parseNameHasOwner(out string) (value, ok bool) {
	f := strings.Fields(strings.NewReplacer("(", " ", ")", " ", ",", " ").Replace(out))
	for i := len(f) - 1; i >= 0; i-- {
		switch f[i] {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

// TrayHint says why no tray icon shows, for the desktop in
// $XDG_CURRENT_DESKTOP: GNOME needs the AppIndicator extension; any other
// desktop needs a bar or panel with a system tray.
type TrayHint int

const (
	TrayHintNone  TrayHint = iota // a tray host runs (or it could not be told)
	TrayHintGNOME                 // GNOME without the AppIndicator extension
	TrayHintOther                 // a desktop or compositor with no tray host running
)

// TrayHintFor is the hint for whether a host is present and the desktop.
func TrayHintFor(present, known bool, currentDesktop string) TrayHint {
	if present || !known {
		return TrayHintNone
	}
	for _, d := range strings.Split(currentDesktop, ":") {
		if strings.EqualFold(d, "GNOME") { // also "ubuntu:GNOME"
			return TrayHintGNOME
		}
	}
	return TrayHintOther
}
