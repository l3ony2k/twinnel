//go:build !darwin && !linux

package main

import (
	"errors"

	"github.com/l3ony2k/duet/core/service"
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
