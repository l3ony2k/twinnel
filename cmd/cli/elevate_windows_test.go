//go:build windows

package main

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/l3ony2k/twinnel/platform/windows/scm"
)

// The elevated copy's output comes back over the pipe and its error
// becomes this copy's; a dismissed prompt is "cancelled". No UAC prompt is
// shown: shellExecuteRunas is a fake that plays the elevated copy.
func TestElevateWindows(t *testing.T) {
	old, oldOut := shellExecuteRunas, stdout
	t.Cleanup(func() { shellExecuteRunas, stdout = old, oldOut })
	var out bytes.Buffer
	stdout = &out
	var gotArgs []string
	shellExecuteRunas = func(exe, params string) (uint32, error) {
		gotArgs = scm.SplitCommandLine(params)
		i := slices.Index(gotArgs, "--output")
		if i < 0 {
			t.Fatal("no --output")
		}
		w, err := os.OpenFile(gotArgs[i+1], os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		w.WriteString("# step 1\ntwinnel: helper install failed: synthetic\n")
		w.Close()
		return 1, nil
	}
	f := &installFlags{yes: true}
	err := elevate("install", f, `C:\Program Files\Twinnel\twinnel.exe`, []string{"helper", "install", "--as-root", "--state-dir", `C:\Program Data\T"w`})
	if err == nil || err.Error() != "helper install failed: synthetic" {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out.String(), "# step 1") {
		t.Errorf("output not relayed: %q", out.String())
	}
	if !slices.Contains(gotArgs, `C:\Program Data\T"w`) || !slices.Contains(gotArgs, "--allow-sid") {
		t.Errorf("args %q", gotArgs)
	}
	shellExecuteRunas = func(string, string) (uint32, error) { return 0, windows.ERROR_CANCELLED }
	if err := elevate("uninstall", f, "x.exe", nil); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancel: %v", err)
	}
	if err := attachOutput(`C:\Windows\System32\drivers\etc\hosts`); err == nil {
		t.Error("a file path was accepted as output")
	}
	if err := attachOutput(outputPipePrefix + `..\x`); err == nil {
		t.Error("a path in the pipe name was accepted")
	}
}
