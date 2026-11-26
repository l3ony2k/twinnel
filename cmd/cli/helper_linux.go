//go:build linux

package main

import (
	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/platform/linux"
)

// socketGroup picks the helper's socket group (replaced in tests).
var socketGroup = func() (string, bool) { return linux.SocketGroup(nil) }

// planHelperInstall: the systemd plan from platform/linux (state dir
// /var/lib/duet, binary /usr/local/libexec/duet-helper, unit duet-helper,
// socket group sudo, else wheel, else a new "duet" group).
func planHelperInstall(helperBin, stateDir, socket, label string) ([]service.Step, error) {
	group, exists := socketGroup()
	return linux.HelperInstallPlan(helperBin, stateDir, socket, label, group, exists)
}

func planHelperUninstall(stateDir, socket, label string) ([]service.Step, error) {
	return linux.HelperUninstallPlan(stateDir, socket, label)
}
