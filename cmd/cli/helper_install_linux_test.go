//go:build linux

package main

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
)

// On Linux a normal user's install re-executes Twinnel through pkexec,
// each word its own argv element (no shell, no quoting).
func TestHelperInstallElevatesPkexec(t *testing.T) {
	f := installFakes(t, 1000)
	old := socketGroup
	socketGroup = func() (string, bool) { return "wheel", true }
	t.Cleanup(func() { socketGroup = old })
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
	old := socketGroup
	socketGroup = func() (string, bool) { return "wheel", true }
	t.Cleanup(func() { socketGroup = old })
	runElevated = func(context.Context, []string, io.Writer, io.Writer) error {
		return exec.Command("/bin/sh", "-c", "exit 126").Run()
	}
	err := runHelperInstall([]string{"--helper-bin", helperBinary(t), "--yes"})
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err = %v", err)
	}
}
