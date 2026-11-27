package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/l3ony2k/twinnel/core/autostart"
	"github.com/l3ony2k/twinnel/platform/darwin/takeoverprobe"
)

// autostartSource reads launchd plists (system daemons and agents, the
// console user's agents), the apps' login items and launchd's disabled
// lists. A development helper with --takeover-services-dir reads only that
// dir and --takeover-home, never the machine.
func autostartSource(o *options) autostart.Source {
	run := takeoverprobe.SafeRunner(8 * time.Second)
	s := &autostart.LaunchdSource{
		JSON: func(ctx context.Context, p string) ([]byte, error) {
			return run(ctx, "/usr/bin/plutil", "-convert", "json", "-o", "-", p)
		},
	}
	if o.takeoverServices != "" {
		s.Dirs = []autostart.LaunchdDir{{Dir: o.takeoverServices, Kind: autostart.LaunchDaemon}}
		if o.takeoverHome != "" {
			s.Dirs = append(s.Dirs, autostart.LaunchdDir{Dir: filepath.Join(o.takeoverHome, "Library", "LaunchAgents"), Kind: autostart.LaunchAgent, User: "501"})
		}
		return s
	}
	uid := consoleUID()
	home := consoleUserHome()
	s.Dirs = []autostart.LaunchdDir{
		{Dir: "/Library/LaunchDaemons", Kind: autostart.LaunchDaemon},
		{Dir: "/Library/LaunchAgents", Kind: autostart.LaunchAgent},
	}
	s.Apps = []string{"/Applications"}
	if uid != "" {
		s.Users = []string{uid}
		s.Dirs = append(s.Dirs, autostart.LaunchdDir{Dir: filepath.Join(home, "Library", "LaunchAgents"), Kind: autostart.LaunchAgent, User: uid})
		s.Apps = append(s.Apps, filepath.Join(home, "Applications"))
	}
	s.Disabled = func(ctx context.Context, domain string) (map[string]bool, error) {
		out, err := run(ctx, "/bin/launchctl", "print-disabled", domain)
		return autostart.ParsePrintDisabled(out), err
	}
	return s
}

// consoleUID is the uid of the user at the console ("" for none).
func consoleUID() string {
	if fi, err := os.Stat("/dev/console"); err == nil {
		if sys, ok := fi.Sys().(*syscall.Stat_t); ok && sys.Uid != 0 {
			return strconv.Itoa(int(sys.Uid))
		}
	}
	return ""
}
