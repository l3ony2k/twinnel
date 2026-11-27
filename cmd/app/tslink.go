package main

import (
	"os/exec"
	"path/filepath"

	"github.com/l3ony2k/twinnel/core/clilink"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/store"
)

// The tailscale CLI a fresh setup installs goes on PATH next to the twinnel
// CLI link (设置 → 本地 Agent → 命令行工具): made after the setup's
// Tailscale step and with 安装命令行工具, removed with 移除 and 完全移除.
// Never over a tailscale someone else installed (core/clilink).

// managedTailscale is the tailscale CLI under the helper's state dir.
func managedTailscale(s status.Snapshot) string {
	dir := store.DefaultDir
	if st := s.Helper.Status; st != nil && st.StateDir != "" {
		dir = st.StateDir
	}
	return filepath.Join(dir, "tailscale", "current", "tailscale")
}

func tailscaleTool(cli string) clilink.Tool {
	t := clilink.Tailscale(filepath.Dir(filepath.Dir(filepath.Dir(cli))))
	t.Target, t.LookPath = cli, exec.LookPath
	return t
}

// linkTailscaleDirs: the folder the twinnel CLI is linked in first, then
// the usual ones.
func (a *app) linkTailscaleDirs() []string {
	var dirs []string
	for _, l := range a.cliLinks() {
		dirs = append(dirs, filepath.Dir(l))
	}
	return append(dirs, cliTargets()...)
}

// removeTailscaleLinks removes Twinnel's tailscale links (any state dir).
func removeTailscaleLinks() {
	tailscaleTool(filepath.Join(store.DefaultDir, "tailscale", "current", "tailscale")).Remove(cliTargets())
}
