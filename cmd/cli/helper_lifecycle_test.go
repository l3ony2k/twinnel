package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/core/uninstall"
)

// lifecycleFakes adds the helper, mihomo's controller and the binaries to
// installFakes.
type lifecycleFakes struct {
	*fakeSystem
	info     *rpc.HelperInfo // helper.info; nil: the helper does not answer
	statuses []rpc.Status    // status.get answers in turn (the last repeats)
	restart  error           // helper.restart's error
	calls    []string
	versions map[string]string // binary → --version
	exists   map[string]bool
	same     bool
	// ctl answers controllerUp: up until "bootout system/<Twinnel mihomo>",
	// then down until "bootstrap" of the original (if startOK).
	ctlUp   bool
	startOK bool
}

func lifecycle(t *testing.T, euid int) *lifecycleFakes {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS plans")
	}
	l := &lifecycleFakes{fakeSystem: installFakes(t, euid), versions: map[string]string{}, exists: map[string]bool{}, ctlUp: true, startOK: true}
	oldRPC, oldVer, oldCtl, oldPoll, oldExists, oldSame, oldTO, oldVT := helperRPC, binaryVersion, controllerUp, pollEvery, fileExistsFn, sameFile, restoreTimeouts, verifyTimeout
	helperRPC = func(_ context.Context, _ string, method string, _, result any) error {
		l.calls = append(l.calls, method)
		var v any
		switch method {
		case rpc.MethodHelperInfo:
			if l.info == nil {
				return errors.New("dial: no such file")
			}
			v = l.info
		case rpc.MethodStatus:
			if len(l.statuses) == 0 {
				return errors.New("dial: no such file")
			}
			v = l.statuses[0]
			if len(l.statuses) > 1 {
				l.statuses = l.statuses[1:]
			}
		case rpc.MethodHelperRestart:
			if l.restart != nil {
				return l.restart
			}
			v = rpc.HelperRestartResult{PID: 10}
		default:
			return errors.New("unexpected " + method)
		}
		b, _ := json.Marshal(v)
		return json.Unmarshal(b, result)
	}
	binaryVersion = func(_ context.Context, bin string) (string, error) {
		if v, ok := l.versions[bin]; ok {
			return v, nil
		}
		return "", errors.New("no binary " + bin)
	}
	controllerUp = func(context.Context, string) bool { return l.ctlUp }
	pollEvery = time.Millisecond
	restoreTimeouts.down, restoreTimeouts.up = 20*time.Millisecond, 20*time.Millisecond
	verifyTimeout = 50 * time.Millisecond
	fileExistsFn = func(p string) bool {
		if v, ok := l.exists[p]; ok {
			return v
		}
		_, err := os.Stat(p)
		return err == nil
	}
	sameFile = func(a, b string) bool { return l.same }
	// The controller follows the launchctl steps.
	applyPlan = func(_ context.Context, steps []service.Step) error {
		l.applied = append(l.applied, steps)
		for _, s := range steps {
			if s.Kind != service.StepRun || len(s.Argv) < 3 || !strings.HasSuffix(s.Argv[0], "launchctl") {
				continue
			}
			switch {
			case s.Argv[1] == "bootout" && strings.Contains(s.Argv[2], "twinnel.mihomo"):
				l.ctlUp = false
			case s.Argv[1] == "bootstrap" && strings.Contains(s.Argv[3], "com.mihomo.proxy"):
				l.ctlUp = l.startOK
			case s.Argv[1] == "bootstrap" && strings.Contains(s.Argv[3], "twinnel.mihomo"):
				l.ctlUp = true
			}
		}
		return nil
	}
	t.Cleanup(func() {
		helperRPC, binaryVersion, controllerUp, pollEvery, fileExistsFn, sameFile, restoreTimeouts, verifyTimeout = oldRPC, oldVer, oldCtl, oldPoll, oldExists, oldSame, oldTO, oldVT
	})
	return l
}

func (l *lifecycleFakes) steps() []string {
	var out []string
	for _, p := range l.applied {
		for _, s := range p {
			out = append(out, s.String())
		}
	}
	return out
}

func hardenedInfo(stateDir string) *rpc.HelperInfo {
	return &rpc.HelperInfo{
		StateDir:   stateDir,
		Mihomo:     rpc.HelperMihomoService{Label: store.DefaultMihomoLabel, Installed: true, Controller: "127.0.0.1:9090"},
		Hardened:   &rpc.HelperHardened{PlanID: "h1", OldLabel: "com.mihomo.proxy", OldPlist: "/Library/LaunchDaemons/com.mihomo.proxy.plist"},
		SecretKeys: []string{store.SecretController},
	}
}

// writeSettings writes a synthetic settings.json into a state dir named
// Twinnel (purge refuses other names).
func writeSettings(t *testing.T, st *store.Settings) string {
	dir := filepath.Join(t.TempDir(), "twinnel")
	os.MkdirAll(dir, 0o700)
	b, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func hardenedSettings() *store.Settings {
	return &store.Settings{
		Mihomo:        store.MihomoSettings{ServiceLabel: store.DefaultMihomoLabel, Controller: "127.0.0.1:9090"},
		Subscriptions: []store.Subscription{{ID: "adopted"}},
		Hardened: &store.Hardening{PlanID: "h1", OldLabel: "com.mihomo.proxy", OldPlist: "/Library/LaunchDaemons/com.mihomo.proxy.plist",
			OldConfigDir: "/Users/someone/mihomo/conf"},
	}
}

func TestUninstallHardenedRefusesWithoutAChoice(t *testing.T) {
	l := lifecycle(t, 501)
	l.info = hardenedInfo(store.DefaultDir)
	err := runHelperUninstall([]string{"--yes"})
	if err == nil || !strings.Contains(err.Error(), "--restore-mihomo") {
		t.Fatalf("err = %v", err)
	}
	if len(l.elevated) != 0 || len(l.applied) != 0 {
		t.Fatal("ran something")
	}
}

func TestUninstallJSONRefusal(t *testing.T) {
	l := lifecycle(t, 501)
	l.info = hardenedInfo(store.DefaultDir)
	if err := runHelperUninstall([]string{"--dry-run", "--json", "--purge"}); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Refusal struct{ Code string }
		Plan    any
	}
	if err := json.Unmarshal(l.out.Bytes(), &out); err != nil || out.Refusal.Code != "needs-restore" || out.Plan != nil {
		t.Fatalf("%v: %s", err, l.out.String())
	}
}

func TestUninstallHardenedElevatesWithFlags(t *testing.T) {
	l := lifecycle(t, 501)
	l.info = hardenedInfo(store.DefaultDir)
	l.exists["/Library/LaunchDaemons/com.mihomo.proxy.plist"] = true
	if err := runHelperUninstall([]string{"--restore-mihomo", "--purge", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if len(l.elevated) != 1 || len(l.applied) != 0 {
		t.Fatalf("elevated %d, applied %d", len(l.elevated), len(l.applied))
	}
	script := l.elevated[0][2]
	for _, want := range []string{"helper uninstall --as-root --yes", "--restore-mihomo", "--purge"} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q: %s", want, script)
		}
	}
	out := l.out.String()
	for _, want := range []string{"Twinnel's mihomo service com.l3ony2k.twinnel.mihomo", "mihomo's original service com.mihomo.proxy", "keychain items twinnel.controller-secret", "wait until 127.0.0.1:9090 answers"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestUninstallAsRootRestoresMihomo(t *testing.T) {
	l := lifecycle(t, 0)
	dir := writeSettings(t, hardenedSettings())
	l.exists["/Library/LaunchDaemons/com.mihomo.proxy.plist"] = true
	if err := runHelperUninstall([]string{"--as-root", "--yes", "--restore-mihomo", "--state-dir", dir}); err != nil {
		t.Fatal(err)
	}
	got := l.steps()
	want := []string{
		"/bin/launchctl bootout system/com.l3ony2k.twinnel.mihomo  # errors ignored",
		"/bin/launchctl enable system/com.mihomo.proxy",
		"/bin/launchctl bootstrap system /Library/LaunchDaemons/com.mihomo.proxy.plist",
		"install -m 0644 /Library/LaunchDaemons/com.l3ony2k.twinnel.mihomo.plist " + filepath.Join(dir, "harden", "com.l3ony2k.twinnel.mihomo.plist"),
		"rm -f /Library/LaunchDaemons/com.l3ony2k.twinnel.mihomo.plist",
	}
	if len(got) < len(want)+1 || !slices.Equal(got[:len(want)], want) {
		t.Fatalf("steps:\n%s", strings.Join(got, "\n"))
	}
	rest := strings.Join(got[len(want):], "\n")
	if !strings.Contains(rest, "bootout system/com.l3ony2k.twinnel.helper") || strings.Contains(rest, "rm -rf") {
		t.Fatalf("helper steps:\n%s", rest)
	}
	assertNoNetworkServices(t, got[len(want):])
}

func TestUninstallAsRootRestoreFailsKeepsHelper(t *testing.T) {
	l := lifecycle(t, 0)
	l.startOK = false
	dir := writeSettings(t, hardenedSettings())
	l.exists["/Library/LaunchDaemons/com.mihomo.proxy.plist"] = true
	err := runHelperUninstall([]string{"--as-root", "--yes", "--restore-mihomo", "--purge", "--state-dir", dir})
	if err == nil || !strings.Contains(err.Error(), "helper was not removed") {
		t.Fatalf("err = %v", err)
	}
	got := strings.Join(l.steps(), "\n")
	if !strings.Contains(got, "bootstrap system /Library/LaunchDaemons/com.l3ony2k.twinnel.mihomo.plist") {
		t.Errorf("Twinnel's mihomo not started again:\n%s", got)
	}
	if strings.Contains(got, "twinnel.helper") || strings.Contains(got, "rm -rf") || strings.Contains(got, "rm -f /Library/LaunchDaemons/com.l3ony2k.twinnel.mihomo.plist") {
		t.Errorf("removed something after a failed restore:\n%s", got)
	}
	if !l.ctlUp {
		t.Error("mihomo is left down")
	}
}

func TestUninstallAsRootRefusesWithoutRestore(t *testing.T) {
	l := lifecycle(t, 0)
	dir := writeSettings(t, hardenedSettings())
	err := runHelperUninstall([]string{"--as-root", "--yes", "--purge", "--state-dir", dir})
	if err == nil || !strings.Contains(err.Error(), "--restore-mihomo") || len(l.applied) != 0 {
		t.Fatalf("err = %v, applied %v", err, l.steps())
	}
}

func TestUninstallAsRootRestoreNeedsOldPlist(t *testing.T) {
	l := lifecycle(t, 0)
	dir := writeSettings(t, hardenedSettings())
	l.exists["/Library/LaunchDaemons/com.mihomo.proxy.plist"] = false
	err := runHelperUninstall([]string{"--as-root", "--yes", "--restore-mihomo", "--state-dir", dir})
	if err == nil || !strings.Contains(err.Error(), "is gone") || len(l.applied) != 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestUninstallAsRootPurgeOwnMihomoRefused(t *testing.T) {
	l := lifecycle(t, 0)
	dir := writeSettings(t, &store.Settings{})
	l.exists["/Library/LaunchDaemons/com.l3ony2k.twinnel.mihomo.plist"] = true
	err := runHelperUninstall([]string{"--as-root", "--yes", "--purge", "--state-dir", dir})
	if err == nil || !strings.Contains(err.Error(), "twinnel service uninstall mihomo") || len(l.applied) != 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestUninstallAsRootPurgeAdopted(t *testing.T) {
	l := lifecycle(t, 0)
	dir := writeSettings(t, &store.Settings{
		Mihomo:        store.MihomoSettings{ServiceLabel: "com.mihomo.proxy", ServiceAdopted: true},
		Subscriptions: []store.Subscription{{ID: "adopted"}},
	})
	if err := runHelperUninstall([]string{"--as-root", "--yes", "--purge", "--state-dir", dir}); err != nil {
		t.Fatal(err)
	}
	got := l.steps()
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"bootout system/com.l3ony2k.twinnel.helper",
		"/usr/bin/security delete-generic-password -a twinnel -s twinnel.controller-secret /Library/Keychains/System.keychain  # errors ignored",
		"-s twinnel.subscription-url/adopted",
		"/bin/rm -rf -- " + dir,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("lacks %q:\n%s", want, joined)
		}
	}
	// The purge comes after the helper is gone.
	if strings.Index(joined, "rm -rf") < strings.Index(joined, "twinnel.helper") {
		t.Error("purge before the helper's removal")
	}
	assertNoNetworkServices(t, got)
}

func TestPurgeRefusesOddStateDir(t *testing.T) {
	for _, dir := range []string{"/", "/Library", "relative/twinnel", "/tmp/x/other"} {
		p := planWithStateDir(dir)
		if _, err := purgeSteps(p, dir); err == nil {
			t.Errorf("%q: no error", dir)
		}
	}
}

// assertNoNetworkServices: no step starts, stops or reloads mihomo or
// tailscaled.
func assertNoNetworkServices(t *testing.T, steps []string) {
	t.Helper()
	for _, s := range steps {
		if strings.Contains(s, "launchctl") && (strings.Contains(s, "mihomo") || strings.Contains(s, "tailscale")) {
			t.Errorf("touches a network service: %s", s)
		}
	}
}

func TestUpgradeInstallsNewerAndVerifies(t *testing.T) {
	l := lifecycle(t, 501)
	bin := helperBinary(t)
	l.versions[bin] = "0.3.0"
	l.versions["/Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper"] = "0.2.0"
	l.exists["/Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper"] = true
	l.statuses = []rpc.Status{{HelperVersion: "0.2.0", PID: 5}, {HelperVersion: "0.3.0", PID: 6}}
	if err := runHelperUpgrade([]string{"--helper-bin", bin, "--yes"}); err != nil {
		t.Fatal(err)
	}
	if len(l.elevated) != 1 {
		t.Fatalf("elevated %d", len(l.elevated))
	}
	script := l.elevated[0][2]
	if !strings.Contains(script, "helper upgrade --as-root --yes --helper-bin "+bin) {
		t.Errorf("script: %s", script)
	}
	out := l.out.String()
	for _, want := range []string{"install -m 0755 " + bin + " /Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper", "bootstrap system /Library/LaunchDaemons/com.l3ony2k.twinnel.helper.plist", "helper 0.3.0 answers"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "mihomo") || strings.Contains(out, "tailscale") {
		t.Errorf("mentions a network service:\n%s", out)
	}
}

func TestUpgradeRefusesDowngrade(t *testing.T) {
	l := lifecycle(t, 501)
	bin := helperBinary(t)
	l.versions[bin] = "0.1.0"
	l.versions["/Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper"] = "0.2.0"
	l.exists["/Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper"] = true
	err := runHelperUpgrade([]string{"--helper-bin", bin, "--yes"})
	if err == nil || !strings.Contains(err.Error(), "--force") || len(l.elevated) != 0 {
		t.Fatalf("err = %v", err)
	}
	l.statuses = []rpc.Status{{HelperVersion: "0.1.0", PID: 6}}
	if err := runHelperUpgrade([]string{"--helper-bin", bin, "--yes", "--force"}); err != nil {
		t.Fatal(err)
	}
	if len(l.elevated) != 1 || !strings.Contains(l.elevated[0][2], " --force") {
		t.Fatalf("elevated %v", l.elevated)
	}
}

func TestUpgradeCheck(t *testing.T) {
	l := lifecycle(t, 501)
	bin := helperBinary(t)
	l.versions[bin] = "0.2.0"
	l.versions["/Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper"] = "0.2.0"
	l.exists["/Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper"] = true
	l.same = true
	l.statuses = []rpc.Status{{HelperVersion: "0.1.0", PID: 5}}
	if err := runHelperUpgrade([]string{"--helper-bin", bin, "--check", "--json"}); err != nil {
		t.Fatal(err)
	}
	var c UpgradeCheck
	if err := json.Unmarshal(l.out.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.Action != UpgradeRestart || c.Running != "0.1.0" || c.Bundled != "0.2.0" {
		t.Fatalf("check = %+v", c)
	}
	// Same bytes, running the same version: nothing to do.
	l.out.Reset()
	l.statuses = []rpc.Status{{HelperVersion: "0.2.0", PID: 5}}
	if err := runHelperUpgrade([]string{"--helper-bin", bin, "--yes"}); err != nil {
		t.Fatal(err)
	}
	if len(l.elevated) != 0 || !strings.Contains(l.out.String(), "up to date") {
		t.Fatalf("elevated %d: %s", len(l.elevated), l.out.String())
	}
}

func TestUpgradeNotInstalled(t *testing.T) {
	l := lifecycle(t, 501)
	bin := helperBinary(t)
	l.versions[bin] = "0.2.0"
	l.exists["/Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper"] = false
	err := runHelperUpgrade([]string{"--helper-bin", bin, "--yes"})
	if err == nil || !strings.Contains(err.Error(), "twinnel helper install") || len(l.elevated) != 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestRestartThroughHelper(t *testing.T) {
	l := lifecycle(t, 501)
	l.statuses = []rpc.Status{{PID: 10}, {PID: 11, HelperVersion: "0.2.0"}}
	if err := runHelperRestart(nil); err != nil {
		t.Fatal(err)
	}
	if len(l.elevated) != 0 || !slices.Contains(l.calls, rpc.MethodHelperRestart) {
		t.Fatalf("elevated %v, calls %v", l.elevated, l.calls)
	}
}

func TestRestartFallsBackToLaunchctl(t *testing.T) {
	l := lifecycle(t, 501)
	l.restart = errors.New("dial: connection refused")
	l.statuses = []rpc.Status{{PID: 11}}
	if err := runHelperRestart([]string{"--yes"}); err != nil {
		t.Fatal(err)
	}
	if len(l.elevated) != 1 || !strings.Contains(l.out.String(), "kickstart -k system/com.l3ony2k.twinnel.helper") {
		t.Fatalf("elevated %v:\n%s", l.elevated, l.out.String())
	}
	assertNoNetworkServices(t, strings.Split(l.out.String(), "\n"))
}

func planWithStateDir(dir string) uninstall.Plan {
	return uninstall.Plan{Remove: []uninstall.Item{{Kind: uninstall.StateDir, Path: dir}}}
}

func TestUninstallAsRootRestoresDNSFirst(t *testing.T) {
	l := lifecycle(t, 0)
	dir := writeSettings(t, &store.Settings{})
	l.exists[filepath.Join(dir, "dns-backup.json")] = true
	var restored string
	old := restoreSystemDNS
	restoreSystemDNS = func(_ context.Context, stateDir string) error {
		if len(l.applied) != 0 {
			t.Error("DNS restored after the helper's removal")
		}
		restored = stateDir
		return nil
	}
	t.Cleanup(func() { restoreSystemDNS = old })
	if err := runHelperUninstall([]string{"--as-root", "--yes", "--state-dir", dir}); err != nil {
		t.Fatal(err)
	}
	if restored != dir || l.tsLinksRemoved != 1 {
		t.Fatalf("restored %q, output:\n%s", restored, l.out.String())
	}
	// A failed restore stops the uninstall: nothing else changes.
	l2 := lifecycle(t, 0)
	l2.exists[filepath.Join(dir, "dns-backup.json")] = true
	restoreSystemDNS = func(context.Context, string) error { return errors.New("networksetup failed") }
	if err := runHelperUninstall([]string{"--as-root", "--yes", "--state-dir", dir}); err == nil || len(l2.applied) != 0 {
		t.Fatalf("err = %v, applied %v", err, l2.applied)
	}
}
