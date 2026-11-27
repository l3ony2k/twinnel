//go:build linux

package main

import (
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/platform/linux"
	"github.com/l3ony2k/twinnel/platform/linux/systemd"
)

// socketGroup picks the helper's socket group (replaced in tests).
var socketGroup = func() (string, bool) { return linux.SocketGroup(nil) }

// planHelperInstall: the systemd plan from platform/linux (state dir
// /var/lib/twinnel, binary /usr/local/libexec/twinnel-helper, unit twinnel-helper,
// socket group sudo, else wheel, else a new "twinnel" group).
func planHelperInstall(helperBin, stateDir, socket, label string) ([]service.Step, error) {
	group, exists := socketGroup()
	return linux.HelperInstallPlan(helperBin, stateDir, socket, label, group, exists)
}

func planHelperUninstall(stateDir, socket, label string) ([]service.Step, error) {
	return linux.HelperUninstallPlan(stateDir, socket, label)
}

func helperPaths(stateDir, label string) (bin, def string) {
	_, label = linux.InstallPaths(stateDir, label)
	return linux.HelperBin, systemd.New().DefinitionPath(label)
}

func serviceDefinition(label string) string { return systemd.New().DefinitionPath(label) }

func planHelperRestart(label string) ([]service.Step, error) {
	_, label = linux.InstallPaths("", label)
	return systemd.New().Plan(service.OpRestart, service.Spec{Label: label})
}

// secretStore: on Linux the helper keeps secrets in a file in its state
// dir, which --purge deletes with it.
const secretStore = "file"

func planSecretsDelete([]string) []service.Step { return nil }

// planMihomoRestore: `twinnel helper harden` is macOS-only, so there is
// nothing to restore.
func planMihomoRestore(from, to, toPlist string) (stop, start, undo []service.Step) {
	return nil, nil, nil
}

// readServiceState is used by the macOS plans only.
func readServiceState(string) (bool, []byte, error) { return false, nil, nil }
