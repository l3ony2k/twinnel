//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/core/store"
	"github.com/l3ony2k/duet/platform/darwin/launchd"
)

const installedHelper = "/Library/PrivilegedHelperTools/com.l3ony2k.duet.helper"

// helperModel is the helper's service as the root run sees it: the steps
// applied change whether launchd has it loaded and which version answers
// on the socket. lostAfterBootstrap reproduces the owner's 0.0.0-298
// upgrade: bootstrap "succeeds" right after the bootout, and the service
// is gone anyway.
type helperModel struct {
	*lifecycleFakes
	answers            string // version on the socket; "" none
	newVersion         string
	lostAfterBootstrap int // bootstraps that end with nothing loaded
	failApply          error
	pid                int
}

func upgradeAsRoot(t *testing.T) (*helperModel, string) {
	l := lifecycle(t, 0)
	m := &helperModel{lifecycleFakes: l, answers: "0.2.0", newVersion: "0.3.0", pid: 5}
	bin := helperBinary(t)
	l.versions[bin] = "0.3.0"
	l.versions[installedHelper] = "0.2.0"
	l.exists[installedHelper] = true
	helperRPC = func(_ context.Context, _ string, method string, _, result any) error {
		l.calls = append(l.calls, method)
		if method != rpc.MethodStatus || m.answers == "" {
			return errors.New("dial unix /var/run/duet/helper.sock: connect: no such file or directory")
		}
		b, _ := json.Marshal(rpc.Status{HelperVersion: m.answers, PID: m.pid})
		return json.Unmarshal(b, result)
	}
	applyPlan = func(_ context.Context, steps []service.Step) error {
		l.applied = append(l.applied, steps)
		for _, s := range steps {
			if s.Kind != service.StepRun || len(s.Argv) < 2 || s.Argv[0] != "/bin/launchctl" {
				continue
			}
			switch s.Argv[1] {
			case "bootout":
				l.loaded, m.answers = false, ""
			case "bootstrap":
				if m.failApply != nil {
					err := m.failApply
					m.failApply = nil
					return err
				}
				if m.lostAfterBootstrap > 0 {
					m.lostAfterBootstrap--
					continue
				}
				l.loaded, m.answers, m.pid = true, m.newVersion, m.pid+1
			case "kickstart":
				if !l.loaded {
					return errors.New(`exit status 113: Could not find service "com.l3ony2k.duet.helper" in domain for system`)
				}
				m.answers, m.pid = m.newVersion, m.pid+1
			}
		}
		return nil
	}
	return m, bin
}

func (m *helperModel) runLines() []string {
	var out []string
	for _, s := range m.steps() {
		if strings.HasPrefix(s, "/bin/launchctl") {
			out = append(out, strings.SplitN(strings.TrimPrefix(s, "/bin/launchctl "), "  #", 2)[0])
		}
	}
	return out
}

func currentPlist(t *testing.T) []byte {
	stateDir := store.DefaultDir
	spec := service.HelperSpec(store.DefaultHelperLabel, installedHelper, stateDir, rpc.DefaultSocket, stateDir+"/logs")
	b, err := launchd.New().Render(spec)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Only the binary changed: launchd restarts it in place; no bootout, no
// bootstrap, nothing that can leave it unloaded.
func TestUpgradeSamePlistKickstartsOnly(t *testing.T) {
	m, bin := upgradeAsRoot(t)
	m.def = currentPlist(t)
	if err := runHelperUpgrade([]string{"--as-root", "--yes", "--helper-bin", bin}); err != nil {
		t.Fatalf("%v\n%s", err, m.out.String())
	}
	if got := strings.Join(m.runLines(), "; "); got != "kickstart -k system/com.l3ony2k.duet.helper" {
		t.Errorf("launchctl: %s", got)
	}
	if !strings.Contains(m.out.String(), "helper 0.3.0 answers") {
		t.Errorf("not verified as root:\n%s", m.out.String())
	}
}

// Another plist: the full plan, which waits for the bootout before the
// bootstrap and retries it.
func TestUpgradeNewPlistWaitsAndRetries(t *testing.T) {
	m, bin := upgradeAsRoot(t)
	m.def = []byte("<old plist/>")
	if err := runHelperUpgrade([]string{"--as-root", "--yes", "--helper-bin", bin}); err != nil {
		t.Fatal(err)
	}
	steps := m.steps()
	var wait, boot int = -1, -1
	for i, s := range steps {
		if strings.HasPrefix(s, "wait until `/bin/launchctl print system/com.l3ony2k.duet.helper`") {
			wait = i
		}
		if strings.Contains(s, "launchctl bootstrap system") {
			boot = i
			if !strings.Contains(s, "retried") {
				t.Errorf("bootstrap not retried: %s", s)
			}
		}
	}
	if wait < 0 || boot < wait {
		t.Errorf("no wait before bootstrap:\n%s", strings.Join(steps, "\n"))
	}
}

// The owner's failure: the plan "succeeds" but the service is gone. The
// root run notices (no answer on the socket), bootstraps again and
// verifies.
func TestUpgradeAsRootRecoversUnloadedHelper(t *testing.T) {
	m, bin := upgradeAsRoot(t)
	m.lostAfterBootstrap = 1
	if err := runHelperUpgrade([]string{"--as-root", "--yes", "--helper-bin", bin}); err != nil {
		t.Fatalf("%v\n%s", err, m.out.String())
	}
	lines := m.runLines()
	if n := len(lines); n < 2 || lines[n-2] != "enable system/com.l3ony2k.duet.helper" ||
		lines[n-1] != "bootstrap system /Library/LaunchDaemons/com.l3ony2k.duet.helper.plist" {
		t.Errorf("recovery: %v", lines)
	}
	out := m.out.String()
	if !strings.Contains(out, "starting its service again") || !strings.Contains(out, "helper 0.3.0 answers") {
		t.Errorf("output:\n%s", out)
	}
}

// A plan that fails (bootstrap refused after its retries) still gets the
// recovery; when that works, the upgrade is done.
func TestUpgradeAsRootApplyFailsThenRecovers(t *testing.T) {
	m, bin := upgradeAsRoot(t)
	m.failApply = errors.New("bootstrap: exit status 5: Bootstrap failed: 5: Input/output error (after 5 attempts)")
	if err := runHelperUpgrade([]string{"--as-root", "--yes", "--helper-bin", bin}); err != nil {
		t.Fatalf("%v\n%s", err, m.out.String())
	}
	if !strings.Contains(m.out.String(), "runs again after the failed step") {
		t.Errorf("output:\n%s", m.out.String())
	}
}

// When nothing brings it back, the error says what launchctl said, loudly.
func TestUpgradeAsRootRecoveryFails(t *testing.T) {
	m, bin := upgradeAsRoot(t)
	m.lostAfterBootstrap = 99
	err := runHelperUpgrade([]string{"--as-root", "--yes", "--helper-bin", bin})
	if err == nil || !strings.Contains(err.Error(), "helper upgrade failed") || !strings.Contains(err.Error(), "starting it again failed too") {
		t.Fatalf("err = %v", err)
	}
}

// 重新启动 on a helper that is installed but not loaded bootstraps it;
// kickstart alone cannot start an unloaded service.
func TestRestartBootstrapsUnloadedHelper(t *testing.T) {
	m, _ := upgradeAsRoot(t)
	m.loaded, m.answers, m.newVersion = false, "", "0.2.0"
	if err := runHelperRestart([]string{"--as-root", "--yes"}); err != nil {
		t.Fatalf("%v\n%s", err, m.out.String())
	}
	if got := strings.Join(m.runLines(), "; "); got != "enable system/com.l3ony2k.duet.helper; bootstrap system /Library/LaunchDaemons/com.l3ony2k.duet.helper.plist" {
		t.Errorf("launchctl: %s", got)
	}
}

// The outer (unprivileged) copy reports the elevated run's own error, not
// a generic "did not complete", so the app shows the reason.
func TestElevatedFailureReachesCaller(t *testing.T) {
	l := lifecycle(t, 501)
	bin := helperBinary(t)
	l.versions[bin] = "0.3.0"
	l.versions[installedHelper] = "0.2.0"
	l.exists[installedHelper] = true
	l.statuses = []rpc.Status{{HelperVersion: "0.2.0", PID: 5}}
	for _, c := range []struct{ stderr, want string }{
		{"0:310: execution error: duet: helper upgrade failed: /bin/launchctl bootstrap system /Library/LaunchDaemons/x.plist: exit status 5: Bootstrap failed: 5: Input/output error (1)",
			"helper upgrade failed: /bin/launchctl bootstrap system /Library/LaunchDaemons/x.plist: exit status 5: Bootstrap failed: 5: Input/output error"},
		{"0:310: execution error: User canceled. (-128)", "cancelled; nothing was changed"},
	} {
		runElevated = func(_ context.Context, _ []string, _, stderr io.Writer) error {
			stderr.Write([]byte(c.stderr + "\n"))
			return exec.Command("/bin/sh", "-c", "exit 1").Run()
		}
		err := runHelperUpgrade([]string{"--helper-bin", bin, "--yes"})
		if err == nil || err.Error() != c.want {
			t.Errorf("err = %v, want %q", err, c.want)
		}
	}
}
