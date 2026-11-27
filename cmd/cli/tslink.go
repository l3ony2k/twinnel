package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/l3ony2k/twinnel/core/clilink"
)

// cliLinkDirs are where command links go: /usr/local/bin, else the user's
// ~/.local/bin (the same folders as the app's 命令行工具).
func cliLinkDirs() []string {
	d := []string{"/usr/local/bin"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		d = append(d, filepath.Join(home, ".local", "bin"))
	}
	return d
}

// tailscaleTool is the link to the tailscale CLI Twinnel installed at cli
// (<state>/tailscale/current/tailscale).
func tailscaleTool(cli string) clilink.Tool {
	t := clilink.Tailscale(filepath.Dir(filepath.Dir(filepath.Dir(cli))))
	t.Target, t.LookPath = cli, exec.LookPath
	return t
}

// Variables for tests: they touch folders on PATH.
var (
	setupLinkTailscale = func(cli string) (string, error) {
		p, err := tailscaleTool(cli).Link(cliLinkDirs())
		if err == nil {
			stdout.Write([]byte("      tailscale command: " + p + "\n"))
		}
		return p, err
	}
	// removeTailscaleLinks removes the links to stateDir's tailscale CLI
	// (helper uninstall). Tailscale itself stays installed.
	removeTailscaleLinks = func(stateDir string) []string {
		return tailscaleTool(filepath.Join(stateDir, "tailscale", "current", "tailscale")).Remove(cliLinkDirs())
	}
)
