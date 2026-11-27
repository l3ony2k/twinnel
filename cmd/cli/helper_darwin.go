//go:build darwin

package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/l3ony2k/twinnel/core/legacy"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/platform/darwin/keychain"
	"github.com/l3ony2k/twinnel/platform/darwin/launchd"
)

// helperInstallDir holds the installed helper binary.
const helperInstallDir = "/Library/PrivilegedHelperTools"

// planHelperInstall lists what installing the helper does on macOS: copy
// the binary, create the state dir, write and load the LaunchDaemon. Over
// a loaded helper whose plist does not change (an upgrade that only
// replaces the binary), launchd restarts it in place instead (kickstart
// -k): no bootout/bootstrap, so nothing can leave it unloaded.
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
	loaded, current, _ := helperServiceState(label) // unknown: the full plan
	svc, err := l.PlanReload(spec, current, loaded)
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

// helperPaths are the installed helper's binary and LaunchDaemon plist.
func helperPaths(stateDir, label string) (bin, def string) {
	return filepath.Join(helperInstallDir, label), launchd.New().DefinitionPath(label)
}

// serviceDefinition is where a system service's plist lives.
func serviceDefinition(label string) string { return launchd.New().DefinitionPath(label) }

// planHelperRestart restarts only the helper's LaunchDaemon, and loads it
// when it is installed but not loaded (kickstart cannot start an unloaded
// service).
func planHelperRestart(label string) ([]service.Step, error) {
	loaded, _, err := helperServiceState(label)
	if err != nil {
		loaded = true // launchctl print failed oddly: try the restart
	}
	return launchd.New().PlanStartOrRestart(label, loaded)
}

// readServiceState asks launchd whether label is loaded (launchctl print,
// a read) and reads its installed plist (nil when missing).
func readServiceState(label string) (loaded bool, def []byte, err error) {
	l := launchd.New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := l.Status(ctx, label)
	def, _ = os.ReadFile(l.DefinitionPath(label))
	return st.Loaded, def, err
}

// secretStore: the installed helper keeps secrets in the System keychain.
const secretStore = "keychain"

// planSecretsDelete deletes the helper's keychain items, and the ones of
// the same keys from before the rename (a missing one is not an error:
// `security` exits 44, ignored).
func planSecretsDelete(keys []string) []service.Step {
	var steps []service.Step
	for _, names := range [][2]string{{keychain.DefaultAccount, keychain.DefaultService}, {legacy.KeychainAccount, legacy.KeychainService}} {
		for _, k := range keys {
			steps = append(steps, service.Step{Kind: service.StepRun, IgnoreError: true, Argv: []string{
				"/usr/bin/security", "delete-generic-password", "-a", names[0],
				"-s", names[1] + "." + k, keychain.SystemKeychain,
			}})
		}
	}
	return steps
}

// planMihomoRestore is the documented move back from Twinnel's mihomo
// LaunchDaemon to the original one (runbook step 8c), in three parts run
// with waits in between: stop Twinnel's, start the original, and undo (start
// Twinnel's again) when the original does not answer.
func planMihomoRestore(from, to, toPlist string) (stop, start, undo []service.Step) {
	lc := "/bin/launchctl"
	run := func(ignore bool, args ...string) service.Step {
		return service.Step{Kind: service.StepRun, Argv: append([]string{lc}, args...), IgnoreError: ignore}
	}
	stop = []service.Step{run(true, "bootout", "system/"+from)}
	start = []service.Step{run(false, "enable", "system/"+to), run(false, "bootstrap", "system", toPlist)}
	undo = []service.Step{
		run(true, "bootout", "system/"+to),
		run(true, "disable", "system/"+to),
		run(true, "enable", "system/"+from),
		run(false, "bootstrap", "system", serviceDefinition(from)),
	}
	return stop, start, undo
}

// legacyInstall is the helper as installed before the rename to Twinnel.
func legacyInstall() migrateSide {
	return migrateSide{
		Label: legacy.HelperLabel, StateDir: legacy.StateDir, Socket: legacy.Socket,
		Bin: legacy.HelperTools, Plist: legacy.HelperPlist,
	}
}

// planHelperStop unloads a helper's LaunchDaemon and waits until launchd
// let go of it. Its plist and binary stay.
func planHelperStop(label string) ([]service.Step, error) { return launchd.New().PlanStop(label) }

// planHelperStart loads an installed, unloaded helper (enable, bootstrap).
func planHelperStart(label string) ([]service.Step, error) {
	return launchd.New().PlanStartOrRestart(label, false)
}

// dropLegacySecrets deletes the keychain items from before the rename
// whose value the current item holds (copied by the new helper when it
// first read them). An item not copied yet stays: the helper still reads
// it from there. Replaceable in tests.
var dropLegacySecrets = func(keys []string) []string {
	cur := &keychain.Keychain{}
	old := &keychain.Keychain{Service: legacy.KeychainService, Account: legacy.KeychainAccount}
	var notes []string
	for _, k := range keys {
		v, err := cur.Get(k)
		if err != nil {
			notes = append(notes, "keychain item "+legacy.KeychainService+"."+k+" kept: "+err.Error())
			continue
		}
		if ov, err := old.Get(k); err != nil || ov != v {
			continue // no old item, or another value: leave it alone
		}
		if err := old.Delete(k); err != nil {
			notes = append(notes, "keychain item "+legacy.KeychainService+"."+k+" kept: "+err.Error())
		}
	}
	return notes
}

// elevation: macOS's administrator password dialog (osascript).
var elevation = osascriptElevator
