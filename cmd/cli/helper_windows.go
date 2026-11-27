//go:build windows

package main

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/l3ony2k/twinnel/core/priv"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/platform/windows/scm"
	"github.com/l3ony2k/twinnel/platform/windows/svchost"
	"github.com/l3ony2k/twinnel/platform/windows/winpath"
)

func init() {
	// Plans run sc.exe steps through the SCM API, the rest with os/exec.
	applyPlan = func(ctx context.Context, steps []service.Step) error {
		return service.Apply(ctx, steps, scm.Executor{Fallback: service.ExecExecutor{Timeout: time.Minute}}, service.OSFiles{})
	}
	serviceHostCheck = windowsHostCheck
	serviceHostPath = func() string { return winpath.SvcHostBin() }
}

// windowsHostCheck: the service host against the helper binary src, and
// its refresh when it is stale (scm.Host). The mihomo service is the one
// Twinnel installs under its default name; the helper also refreshes the
// host when it starts, with the name in its settings.
func windowsHostCheck(ctx context.Context, src string) (*HostCheck, []service.Step) {
	h := hostReader.Read(ctx, "", src, []string{store.DefaultMihomoLabel})
	if !h.Present {
		return nil, nil
	}
	hc := &HostCheck{Path: h.Path, Version: h.Version, Action: HostNone}
	if !h.Stale(svchost.Revision) {
		return hc, nil
	}
	hc.Action, hc.Restarts = HostRefresh, h.Restarts(svchost.Revision)
	return hc, h.RefreshPlan(src, svchost.Revision, time.Now())
}

// hostReader reads the service host (replaced in tests).
var hostReader = scm.DefaultHostReader()

// planHelperInstall: the TwinnelHelper service (scm.HelperInstallPlan). The
// pipe allows the --allow-sid users; without any (the copy that prints the
// plan before asking for elevation), the user running Twinnel, which is
// what that copy passes on. An upgrade keeps the SIDs the installed
// helper already allows.
func planHelperInstall(helperBin, stateDir, socket, label string) ([]service.Step, error) {
	sids := slices.Clone([]string(allowSIDs))
	if len(sids) == 0 {
		if self, err := priv.CurrentUserSID(); err == nil && !priv.Elevated() {
			sids = append(sids, self)
		}
	}
	if cl, err := scm.ServiceCommandLine(label); err == nil {
		for _, s := range scm.AllowedSIDs(cl) {
			if !slices.Contains(sids, s) {
				sids = append(sids, s)
			}
		}
	}
	return scm.HelperInstallPlan(helperBin, stateDir, socket, label, sids)
}

func planHelperUninstall(stateDir, socket, label string) ([]service.Step, error) {
	return scm.HelperUninstallPlan(label)
}

// helperPaths are the installed helper's binary and its service's
// registry key.
func helperPaths(stateDir, label string) (bin, def string) {
	return winpath.HelperBin(), (&scm.Manager{}).DefinitionPath(label)
}

func serviceDefinition(label string) string { return (&scm.Manager{}).DefinitionPath(label) }

// serviceInstalled asks the SCM (read-only).
func serviceInstalled(label string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := (&scm.Manager{}).Status(ctx, label)
	return err == nil && st.Installed
}

func planHelperRestart(label string) ([]service.Step, error) {
	return (&scm.Manager{}).Plan(service.OpRestart, service.Spec{Label: label})
}

// removeTreeStep deletes the state dir on --purge (no rm on Windows).
func removeTreeStep(dir string) service.Step {
	return service.Step{Kind: service.StepRemoveAll, Path: dir}
}

// secretStore: the DPAPI-sealed secrets live in the state dir, which
// --purge deletes with it.
const secretStore = "file"

func planSecretsDelete([]string) []service.Step { return nil }

// planMihomoRestore: there is no harden on Windows.
func planMihomoRestore(from, to, toPlist string) (stop, start, undo []service.Step) {
	return nil, nil, nil
}

// readServiceState is used by the macOS plans only.
func readServiceState(string) (bool, []byte, error) { return false, nil, nil }

// legacyInstall: there was no Windows install before the rename.
func legacyInstall() migrateSide { return migrateSide{} }

func planHelperStop(string) ([]service.Step, error) {
	return nil, errors.New("migrating a helper from before the rename is only implemented for macOS")
}

func planHelperStart(string) ([]service.Step, error) {
	return nil, errors.New("migrating a helper from before the rename is only implemented for macOS")
}

var dropLegacySecrets = func([]string) []string { return nil }
