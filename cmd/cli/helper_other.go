//go:build !darwin && !linux

package main

import (
	"errors"

	"github.com/l3ony2k/twinnel/core/service"
)

func planHelperInstall(helperBin, stateDir, socket, label string) ([]service.Step, error) {
	return nil, errors.New("helper install is only implemented for macOS so far")
}

func planHelperUninstall(stateDir, socket, label string) ([]service.Step, error) {
	return nil, errors.New("helper uninstall is only implemented for macOS so far")
}

func helperPaths(stateDir, label string) (bin, def string) { return "", "" }

func serviceDefinition(label string) string { return "" }

func planHelperRestart(label string) ([]service.Step, error) {
	return nil, errors.New("helper restart is only implemented for macOS and Linux so far")
}

const secretStore = "file"

func planSecretsDelete([]string) []service.Step { return nil }

func planMihomoRestore(from, to, toPlist string) (stop, start, undo []service.Step) {
	return nil, nil, nil
}

// readServiceState is used by the macOS plans only.
func readServiceState(string) (bool, []byte, error) { return false, nil, nil }

// legacyInstall: there is no install from before the rename to migrate
// here (the rename happened before a release for this platform).
func legacyInstall() migrateSide { return migrateSide{} }

func planHelperStop(string) ([]service.Step, error) {
	return nil, errors.New("migrating a helper from before the rename is only implemented for macOS")
}

func planHelperStart(string) ([]service.Step, error) {
	return nil, errors.New("migrating a helper from before the rename is only implemented for macOS")
}

var dropLegacySecrets = func([]string) []string { return nil }

// elevation: none yet on this platform; run Twinnel as an administrator.
var elevation = elevator{
	argv: func(string, []string) ([]string, error) {
		return nil, errors.New("run this as an administrator: elevation is not implemented on this platform")
	},
	prompt:  "",
	failure: func(int, string) (string, bool, bool) { return "", false, false },
}
