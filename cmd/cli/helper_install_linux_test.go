//go:build linux

package main

import (
	"context"
	"io"
	"os/exec"
	"os/user"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/platform/linux"
)

// On Linux a normal user's install re-executes Twinnel through pkexec,
// each word its own argv element (no shell, no quoting).
func TestHelperInstallElevatesPkexec(t *testing.T) {
	f := installFakes(t, 1000)
	accountFakes(t, "wheel", true)
	bin := helperBinary(t)
	if err := runHelperInstall([]string{"--helper-bin", bin, "--yes"}); err != nil {
		t.Fatal(err)
	}
	if len(f.applied) != 0 || len(f.elevated) != 1 {
		t.Fatalf("applied %d elevated %d", len(f.applied), len(f.elevated))
	}
	argv := f.elevated[0]
	if argv[0] != "/usr/bin/pkexec" || argv[1] != "/Applications/twinnel app/twinnel" || argv[2] != "helper" || argv[3] != "install" || argv[4] != "--as-root" {
		t.Fatalf("argv %q", argv)
	}
	if out := f.out.String(); !strings.Contains(out, "polkit will ask") || !strings.Contains(out, "twinnel-helper.service") {
		t.Errorf("output:\n%s", out)
	}
}

// pkexec's 126 is the dialog dismissed: nothing changed.
func TestHelperInstallPkexecCancelled(t *testing.T) {
	installFakes(t, 1000)
	accountFakes(t, "wheel", true)
	runElevated = func(context.Context, []string, io.Writer, io.Writer) error {
		return exec.Command("/bin/sh", "-c", "exit 126").Run()
	}
	err := runHelperInstall([]string{"--helper-bin", helperBinary(t), "--yes"})
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err = %v", err)
	}
}

// accountFakes: the socket group is group (existing), the invoking user is
// alice (uid 1000), in it or not.
func accountFakes(t *testing.T, group string, member bool) {
	t.Helper()
	oldG, oldA, oldU := socketGroup, accounts, invokingUID
	socketGroup = func() (string, bool) { return group, true }
	invokingUID = func() (string, bool) { return "1000", true }
	accounts = linux.Accounts{
		LookupID:    func(string) (*user.User, error) { return &user.User{Uid: "1000", Gid: "1000", Username: "alice"}, nil },
		LookupGroup: func(string) (*user.Group, error) { return &user.Group{Gid: "998", Name: group}, nil },
		GroupIds: func(*user.User) ([]string, error) {
			if member {
				return []string{"1000", "998"}, nil
			}
			return []string{"1000"}, nil
		},
	}
	t.Cleanup(func() { socketGroup, accounts, invokingUID = oldG, oldA, oldU })
}

func hasUsermod(steps []service.Step, group, name string) bool {
	for _, st := range steps {
		if strings.Join(st.Argv, " ") == linux.Usermod+" -a -G "+group+" "+name {
			return true
		}
	}
	return false
}

// A user outside the socket group is added by the install, which then
// says to log in again.
func TestHelperInstallAddsUserToSocketGroup(t *testing.T) {
	f := installFakes(t, 1000)
	accountFakes(t, "wheel", false)
	if err := runHelperInstall([]string{"--helper-bin", helperBinary(t), "--yes"}); err != nil {
		t.Fatal(err)
	}
	out := f.out.String()
	if !strings.Contains(out, "usermod -a -G wheel alice") || !strings.Contains(out, "alice was added to group wheel: log out and back in") {
		t.Errorf("output:\n%s", out)
	}
}

// As root (the pkexec'd copy), the plan applied has the usermod step; a
// member gets none.
func TestHelperInstallAsRootUsermod(t *testing.T) {
	for _, member := range []bool{false, true} {
		f := installFakes(t, 0)
		accountFakes(t, "wheel", member)
		if err := runHelperInstall([]string{"--helper-bin", helperBinary(t), "--as-root", "--yes"}); err != nil {
			t.Fatal(err)
		}
		if len(f.applied) != 1 || hasUsermod(f.applied[0], "wheel", "alice") == member {
			t.Errorf("member %v: applied %+v", member, f.applied)
		}
		if strings.Contains(f.out.String(), "log out and back in") {
			t.Errorf("member %v: the root copy printed the relogin note", member)
		}
	}
}

// helper upgrade with a current helper still adds a user outside the
// group (and only that); with the user in it, it does nothing.
func TestHelperUpgradeCurrentAddsUser(t *testing.T) {
	f := installFakes(t, 1000)
	accountFakes(t, "wheel", false)
	fl := &installFlags{yes: true, helperBin: "/x/twinnel-helper", stateDir: "/var/lib/twinnel", socket: "/var/run/twinnel/helper.sock", label: "twinnel-helper"}
	if err := helperUpgrade(context.Background(), fl, UpgradeCheck{Action: UpgradeNone}, false); err != nil {
		t.Fatal(err)
	}
	if len(f.elevated) != 1 || !strings.Contains(f.out.String(), "log out and back in") {
		t.Fatalf("elevated %d, output:\n%s", len(f.elevated), f.out.String())
	}

	f = installFakes(t, 1000)
	accountFakes(t, "wheel", true)
	if err := helperUpgrade(context.Background(), fl, UpgradeCheck{Action: UpgradeNone}, false); err != nil {
		t.Fatal(err)
	}
	if len(f.elevated) != 0 {
		t.Errorf("a member was elevated for nothing: %q", f.elevated)
	}
}
