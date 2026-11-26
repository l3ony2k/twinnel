//go:build darwin

package main

import (
	"path/filepath"

	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/platform/darwin/launchd"
)

// helperInstallDir holds the installed helper binary.
const helperInstallDir = "/Library/PrivilegedHelperTools"

// planHelperInstall lists what installing the helper does on macOS: copy
// the binary, create the state dir, write and load the LaunchDaemon.
func planHelperInstall(helperBin, stateDir, socket, label string) ([]service.Step, error) {
	l := launchd.New()
	dst := filepath.Join(helperInstallDir, label)
	logDir := filepath.Join(stateDir, "logs")
	spec := service.HelperSpec(label, dst, stateDir, socket, logDir)
	steps := []service.Step{
		{Kind: service.StepMkdir, Path: helperInstallDir, Mode: 0o755},
		{Kind: service.StepCopy, Src: helperBin, Path: dst, Mode: 0o755},
		{Kind: service.StepMkdir, Path: stateDir, Mode: 0o755},
		{Kind: service.StepMkdir, Path: filepath.Dir(socket), Mode: 0o755},
	}
	svc, err := l.Plan(service.OpInstall, spec)
	if err != nil {
		return nil, err
	}
	return append(steps, svc...), nil
}

// planHelperUninstall unloads the LaunchDaemon, removes the plist, the
// installed binary and the socket. The state dir is kept.
func planHelperUninstall(stateDir, socket, label string) ([]service.Step, error) {
	steps, err := launchd.New().Plan(service.OpUninstall, service.Spec{Label: label})
	if err != nil {
		return nil, err
	}
	return append(steps,
		service.Step{Kind: service.StepRemove, Path: filepath.Join(helperInstallDir, label)},
		service.Step{Kind: service.StepRemove, Path: socket},
	), nil
}
