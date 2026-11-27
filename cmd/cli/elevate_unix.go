//go:build darwin || linux

package main

import (
	"errors"
	"os"

	"github.com/l3ony2k/twinnel/core/service"
)

// platformEUID is the effective uid (0: root).
func platformEUID() int { return os.Geteuid() }

// elevate re-runs Twinnel as root: osascript's password dialog on macOS,
// polkit's pkexec on Linux (elevation, helper_<os>.go).
func elevate(op string, f *installFlags, exe string, rargs []string) error {
	return elevateWith(elevation, op, f, exe, rargs)
}

// attachOutput is Windows-only (the elevated copy's output pipe).
func attachOutput(path string) error {
	if path != "" {
		return errors.New("--output is only used on Windows")
	}
	return nil
}

// serviceInstalled: a service is installed when its definition file
// (plist, unit) exists.
func serviceInstalled(label string) bool { return fileExistsFn(serviceDefinition(label)) }

// removeTreeStep deletes the state dir on --purge.
func removeTreeStep(dir string) service.Step {
	return service.Step{Kind: service.StepRun, Argv: []string{"/bin/rm", "-rf", "--", dir}}
}
