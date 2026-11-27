package main

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/l3ony2k/twinnel/core/autostart"
)

// autostartSource reads systemd units (system, and each user's) and XDG
// autostart files. A development helper with --takeover-home reads only
// that home.
func autostartSource(o *options) autostart.Source {
	if o.takeoverHome != "" {
		return &autostart.LinuxSource{Homes: []autostart.LinuxHome{{Home: o.takeoverHome, User: filepath.Base(o.takeoverHome)}}}
	}
	s := &autostart.LinuxSource{
		SystemDirs: []string{"/etc/systemd/system", "/usr/lib/systemd/system", "/lib/systemd/system"},
		WantsRoot:  "/etc/systemd/system",
		XDGDirs:    []string{"/etc/xdg/autostart"},
	}
	homes, _ := filepath.Glob("/home/*")
	for _, h := range homes {
		name := filepath.Base(h)
		if u, err := user.Lookup(name); err != nil || strings.TrimRight(u.HomeDir, "/") != h {
			continue
		}
		if fi, err := os.Stat(h); err == nil && fi.IsDir() {
			s.Homes = append(s.Homes, autostart.LinuxHome{Home: h, User: name})
		}
	}
	return s
}
