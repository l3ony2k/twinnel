package linux

import (
	"path/filepath"

	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/platform/linux/systemd"
)

// InstallPaths maps the CLI's flag defaults, which are the macOS ones
// (store.DefaultDir, store.DefaultHelperLabel), to StateDir and HelperUnit.
// Values the user passed explicitly are kept.
func InstallPaths(stateDir, label string) (string, string) {
	if stateDir == store.DefaultDir {
		stateDir = StateDir
	}
	if label == store.DefaultHelperLabel {
		label = HelperUnit
	}
	return stateDir, label
}

// HelperInstallPlan lists what installing the helper does on Linux: copy
// the binary to HelperBin, create the state dir and the socket dir, create
// the socket group when it does not exist yet, write
// /etc/systemd/system/<label>.service (helper started with --group), then
// daemon-reload, enable and restart. The helper writes its own log to
// <state>/logs/helper.log; its stdout/stderr (a crash) go to the journal.
func HelperInstallPlan(helperBin, stateDir, socket, label, group string, groupExists bool) ([]service.Step, error) {
	stateDir, label = InstallPaths(stateDir, label)
	spec := service.HelperSpec(label, HelperBin, stateDir, socket, "")
	spec.Args = append(spec.Args, "--group", group)
	steps := []service.Step{
		{Kind: service.StepMkdir, Path: filepath.Dir(HelperBin), Mode: 0o755},
		{Kind: service.StepCopy, Src: helperBin, Path: HelperBin, Mode: 0o755},
		{Kind: service.StepMkdir, Path: stateDir, Mode: 0o755},
		{Kind: service.StepMkdir, Path: filepath.Dir(socket), Mode: 0o755},
	}
	if !groupExists {
		steps = append(steps, service.Step{Kind: service.StepRun, Argv: []string{"/usr/sbin/groupadd", "--system", group}})
	}
	svc, err := systemd.New().Plan(service.OpInstall, spec)
	if err != nil {
		return nil, err
	}
	return append(steps, svc...), nil
}

// HelperUninstallPlan disables and removes the unit, the installed binary
// and the socket. The state dir and the socket group are kept.
func HelperUninstallPlan(stateDir, socket, label string) ([]service.Step, error) {
	_, label = InstallPaths(stateDir, label)
	steps, err := systemd.New().Plan(service.OpUninstall, service.Spec{Label: label})
	if err != nil {
		return nil, err
	}
	return append(steps,
		service.Step{Kind: service.StepRemove, Path: HelperBin},
		service.Step{Kind: service.StepRemove, Path: socket},
	), nil
}
