package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/service"
)

// fakeSystem replaces every side effect of helper install/uninstall.
type fakeSystem struct {
	euid     int
	elevated [][]string
	applied  [][]service.Step
	out      bytes.Buffer
}

func installFakes(t *testing.T, euid int) *fakeSystem {
	f := &fakeSystem{euid: euid}
	oldE, oldX, oldR, oldA, oldIn, oldOut := geteuid, executable, runElevated, applyPlan, stdin, stdout
	geteuid = func() int { return f.euid }
	executable = func() (string, error) { return "/Applications/duet app/duet", nil }
	runElevated = func(_ context.Context, argv []string, _, _ io.Writer) error {
		f.elevated = append(f.elevated, argv)
		return nil
	}
	applyPlan = func(_ context.Context, steps []service.Step) error {
		f.applied = append(f.applied, steps)
		return nil
	}
	stdin, stdout = strings.NewReader(""), &f.out
	t.Cleanup(func() {
		geteuid, executable, runElevated, applyPlan, stdin, stdout = oldE, oldX, oldR, oldA, oldIn, oldOut
	})
	return f
}

func helperBinary(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "duet-helper")
	os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	return p
}

func TestHelperInstallElevates(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS plan")
	}
	f := installFakes(t, 501)
	bin := helperBinary(t)
	if err := runHelperInstall([]string{"--helper-bin", bin, "--yes"}); err != nil {
		t.Fatal(err)
	}
	if len(f.applied) != 0 || len(f.elevated) != 1 {
		t.Fatalf("applied %d elevated %d", len(f.applied), len(f.elevated))
	}
	argv := f.elevated[0]
	want := `do shell script "'/Applications/duet app/duet' helper install --as-root --yes --helper-bin ` + bin +
		` --state-dir '/Library/Application Support/duet' --socket /var/run/duet/helper.sock --label com.l3ony2k.duet.helper" with administrator privileges`
	if argv[0] != "/usr/bin/osascript" || argv[1] != "-e" || argv[2] != want {
		t.Errorf("argv\n got %q\nwant %q", argv, want)
	}
	out := f.out.String()
	for _, s := range []string{"will run these steps as root", "<key>Label</key>", "bootstrap system", "administrator password"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	// A label disabled earlier only bootstraps after `launchctl enable`.
	if en, bs := strings.Index(out, "enable system/"), strings.Index(out, "bootstrap system"); en < 0 || en > bs {
		t.Errorf("enable must come before bootstrap:\n%s", out)
	}
}

func TestHelperInstallNeedsConsent(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS plan")
	}
	f := installFakes(t, 501)
	stdin = strings.NewReader("n\n")
	if err := runHelperInstall([]string{"--helper-bin", helperBinary(t)}); err == nil || len(f.elevated) != 0 {
		t.Fatalf("ran without consent: %v", err)
	}
	stdin = strings.NewReader("y\n")
	if err := runHelperInstall([]string{"--helper-bin", helperBinary(t)}); err != nil || len(f.elevated) != 1 {
		t.Fatalf("consent: %v", err)
	}
	if err := runHelperInstall([]string{"--helper-bin", "/nonexistent/duet-helper", "--yes"}); err == nil || len(f.elevated) != 1 {
		t.Errorf("missing binary: %v", err)
	}
	if err := runHelperInstall([]string{"--dry-run", "--helper-bin", "/nonexistent/duet-helper"}); err != nil || len(f.elevated) != 1 {
		t.Errorf("dry run: %v", err)
	}
}

func TestHelperInstallAsRoot(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS plan")
	}
	f := installFakes(t, 501)
	if err := runHelperInstall([]string{"--as-root", "--helper-bin", helperBinary(t)}); err == nil {
		t.Fatal("--as-root accepted without root")
	}
	f.euid = 0
	if err := runHelperInstall([]string{"--as-root", "--yes", "--helper-bin", helperBinary(t)}); err != nil {
		t.Fatal(err)
	}
	if len(f.applied) != 1 || len(f.elevated) != 0 {
		t.Fatalf("applied %d elevated %d", len(f.applied), len(f.elevated))
	}
	var kinds []string
	for _, s := range f.applied[0] {
		kinds = append(kinds, string(s.Kind))
	}
	if got := strings.Join(kinds, ","); got != "mkdir,copy,mkdir,mkdir,mkdir,run,write,run,run" {
		t.Errorf("steps %s", got)
	}

	if err := runHelperUninstall([]string{"--as-root", "--yes"}); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, s := range f.applied[1] {
		lines = append(lines, s.String())
	}
	want := []string{
		"/bin/launchctl bootout system/com.l3ony2k.duet.helper  # errors ignored",
		"rm -f /Library/LaunchDaemons/com.l3ony2k.duet.helper.plist",
		"rm -f /Library/PrivilegedHelperTools/com.l3ony2k.duet.helper",
		"rm -f /var/run/duet/helper.sock",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("uninstall\n%s", strings.Join(lines, "\n"))
	}
}

// TestOsascriptQuoting runs the generated script for real, but without
// "with administrator privileges", against an echo script whose path and
// arguments contain every troublesome character. Nothing is elevated.
func TestOsascriptQuoting(t *testing.T) {
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("no osascript")
	}
	dir := filepath.Join(t.TempDir(), `dir with 'single' "double" \back $HOME`)
	os.MkdirAll(dir, 0o755)
	exe := filepath.Join(dir, "echo-args")
	os.WriteFile(exe, []byte("#!/bin/sh\nfor a in \"$@\"; do printf '[%s]\\n' \"$a\"; done\n"), 0o755)
	args := []string{"plain", "with space", `it's`, `"q"`, `back\slash`, "$(touch /tmp/pwned)", "`id`", "semi;colon", "*", "ünï"}
	argv, err := osascriptArgv(exe, args)
	if err != nil {
		t.Fatal(err)
	}
	script, ok := strings.CutSuffix(argv[2], " with administrator privileges")
	if !ok {
		t.Fatalf("script %q", argv[2])
	}
	out, err := exec.Command(argv[0], argv[1], script).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var want []string
	for _, a := range args {
		want = append(want, "["+a+"]")
	}
	// do shell script returns output with \r line endings.
	got := strings.ReplaceAll(strings.TrimSpace(string(out)), "\r", "\n")
	if got != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
	if _, err := osascriptArgv(exe, []string{"a\nb"}); err == nil {
		t.Error("newline accepted")
	}
}
