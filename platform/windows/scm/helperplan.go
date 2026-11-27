package scm

import (
	"slices"

	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/platform/windows/winpath"
)

// HelperInstallPlan installs or upgrades the helper in place: stop the
// running helper and wait until the SCM reports it stopped (Windows keeps
// a running .exe locked), copy the binary to %ProgramFiles%\Twinnel,
// create the state dir (the helper sets its DACL when it starts), then
// create-or-update the TwinnelHelper service (LocalSystem, automatic,
// restarted on failure) and start it. allowSIDs become --allow-sid: the
// users the helper's pipe lets in besides administrators.
func HelperInstallPlan(helperBin, stateDir, pipe, label string, allowSIDs []string) ([]service.Step, error) {
	dst := winpath.HelperBin()
	spec := service.HelperSpec(label, dst, stateDir, pipe, "")
	for _, s := range allowSIDs {
		spec.Args = append(spec.Args, "--allow-sid", s)
	}
	svc, err := (&Manager{}).Plan(service.OpInstall, spec)
	if err != nil {
		return nil, err
	}
	// svc starts with the stop and the wait for STOPPED.
	files := []service.Step{
		{Kind: service.StepMkdir, Path: winpath.InstallDir(), Mode: 0o755},
		{Kind: service.StepCopy, Src: helperBin, Path: dst, Mode: 0o755},
		{Kind: service.StepMkdir, Path: stateDir, Mode: 0o700},
	}
	return slices.Concat(svc[:2], files, svc[2:]), nil
}

// HelperUninstallPlan stops and deletes the helper's service and removes
// its binary. The state dir stays (--purge removes it), and so does the
// mihomo service with its own host copy.
func HelperUninstallPlan(label string) ([]service.Step, error) {
	steps, err := (&Manager{}).Plan(service.OpUninstall, service.Spec{Name: "helper", Label: label})
	if err != nil {
		return nil, err
	}
	return append(steps, service.Step{Kind: service.StepRemove, Path: winpath.HelperBin()}), nil
}

// AllowedSIDs reads the --allow-sid values of a service command line (the
// installed helper's), so an upgrade by another administrator keeps them.
func AllowedSIDs(cmdline string) []string {
	args := SplitCommandLine(cmdline)
	var out []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--allow-sid" {
			out = append(out, args[i+1])
			i++
		}
	}
	return out
}
