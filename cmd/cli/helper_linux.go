//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/platform/linux"
	"github.com/l3ony2k/twinnel/platform/linux/systemd"
)

// socketGroup picks the helper's socket group (replaced in tests).
var socketGroup = func() (string, bool) { return linux.SocketGroup(nil) }

// planHelperInstall: the systemd plan from platform/linux (state dir
// /var/lib/twinnel, binary /usr/local/libexec/twinnel-helper, unit twinnel-helper,
// socket group sudo, else wheel, else a new "twinnel" group).
// The plan also puts the person installing (PKEXEC_UID, SUDO_UID, or the
// user running it) in that group when they are not in it yet: else their
// app cannot reach the socket (socketAccess).
func planHelperInstall(helperBin, stateDir, socket, label string) ([]service.Step, error) {
	group, exists := socketGroup()
	access, _ := socketAccess(group)
	return linux.HelperInstallPlan(helperBin, stateDir, socket, label, group, exists, access)
}

// Replaceable in tests.
var (
	accounts    = linux.SystemAccounts
	invokingUID = func() (string, bool) { return linux.InvokingUID(os.Getenv, os.Getuid()) }
)

// socketAccess is the step that adds the invoking user to group, and that
// user's name; none when they are in it (or there is nobody but root). A
// failed lookup is printed and adds nothing: the install goes on.
func socketAccess(group string) ([]service.Step, string) {
	uid, ok := invokingUID()
	steps, name, err := linux.SocketAccessSteps(accounts, uid, ok, group)
	if err != nil {
		fmt.Fprintf(stdout, "# could not tell whether you are in group %s: %v\n", group, err)
		return nil, ""
	}
	return steps, name
}

// planSocketAccess is socketAccess for the group the helper uses: what
// `helper upgrade` runs when the helper itself is current.
func planSocketAccess() []service.Step {
	group, _ := socketGroup()
	steps, _ := socketAccess(group)
	return steps
}

// reloginNote tells the person, after steps ran, that they were added to
// the socket group and must log in again before the app can reach the
// helper; it reports whether it did.
func reloginNote(steps []service.Step) bool {
	for _, st := range steps {
		if len(st.Argv) == 5 && st.Argv[0] == linux.Usermod {
			fmt.Fprintf(stdout, "# %s was added to group %s: log out and back in for Twinnel to reach the helper\n", st.Argv[4], st.Argv[3])
			return true
		}
	}
	return false
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

// elevation: polkit's pkexec, which asks the session's authentication
// agent for an administrator's password. argv words are not quoted: no
// shell runs between pkexec and Twinnel.
var elevation = elevator{
	argv:    linux.PkexecArgv,
	prompt:  "polkit will ask for an administrator password",
	failure: linux.PkexecFailure,
}
