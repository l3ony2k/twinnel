package enginetest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Each platform gets a fake it can run: the shell script on macOS and
// Linux, a batch file on Windows, which has no /bin/sh (ui-review 8.3).
func TestFakeMihomoFile(t *testing.T) {
	if n, c := FakeMihomoFile("darwin"); n != "mihomo" || c != FakeMihomoScript {
		t.Errorf("darwin: %s", n)
	}
	n, c := FakeMihomoFile("windows")
	if n != "mihomo.cmd" || !strings.Contains(c, MarkInvalid) || strings.Contains(strings.ReplaceAll(c, "\r\n", ""), "\n") {
		t.Errorf("windows: %s, CRLF lines and the marker expected", n)
	}
}

// The fake on this platform answers -v and -t as mihomo does.
func TestWriteFakeMihomoRuns(t *testing.T) {
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("no sh")
		}
	}
	dir := t.TempDir()
	bin, err := WriteFakeMihomo(dir)
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		out, err := exec.Command(bin, args...).CombinedOutput()
		return string(out), err
	}
	if out, err := run("-v"); err != nil || !strings.Contains(out, "Mihomo Meta") {
		t.Errorf("-v: %v %q", err, out)
	}
	good := filepath.Join(dir, "good config.yaml")
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(good, []byte("mode: rule\n"), 0o644)
	os.WriteFile(bad, []byte("# "+MarkInvalid+"\n"), 0o644)
	if out, err := run("-t", "-d", dir, "-f", good); err != nil || !strings.Contains(out, "test is successful") {
		t.Errorf("-t good: %v %q", err, out)
	}
	if out, err := run("-t", "-f", bad, "-d", dir); err == nil || !strings.Contains(out, "test failed") {
		t.Errorf("-t bad: %v %q", err, out)
	}
	if _, err := run("-t", "-f", filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("-t on a missing file passed")
	}
	if _, err := run("-x"); err == nil {
		t.Error("unsupported arguments passed")
	}
}
