//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/l3ony2k/twinnel/core/priv"
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
	// The fake elevated copy opens the pipe the moment it is started, with
	// a plain open that does not retry: the pipe's instance must already
	// accept a client (an instance that is not there yet, or busy, fails
	// it with ERROR_PIPE_BUSY). Many runs, to catch the race.
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
	for run := 0; run < 50; run++ {
		out.Reset()
		err := elevate("install", f, `C:\Program Files\Twinnel\twinnel.exe`, []string{"helper", "install", "--as-root", "--state-dir", `C:\Program Data\T"w`})
		if err == nil || err.Error() != "helper install failed: synthetic" {
			t.Fatalf("run %d: err = %v", run, err)
		}
		if !strings.Contains(out.String(), "# step 1") {
			t.Fatalf("run %d: output not relayed: %q", run, out.String())
		}
	}
	if !slices.Contains(gotArgs, `C:\Program Data\T"w`) || !slices.Contains(gotArgs, "--allow-sid") {
		t.Errorf("args %q", gotArgs)
	}
	// A copy that never connects (a dismissed prompt) does not keep
	// elevate waiting for the pipe.
	shellExecuteRunas = func(string, string) (uint32, error) { return 0, windows.ERROR_CANCELLED }
	start := time.Now()
	if err := elevate("uninstall", f, "x.exe", nil); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancel: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("cancel took %v: the unused pipe held elevate", d)
	}
	if err := attachOutput(`C:\Windows\System32\drivers\etc\hosts`); err == nil {
		t.Error("a file path was accepted as output")
	}
	if err := attachOutput(outputPipePrefix + `..\x`); err == nil {
		t.Error("a path in the pipe name was accepted")
	}
}

// The elevated copy's end waits for a pipe that is not there yet or busy,
// up to its deadline, and gives up after it.
func TestOpenOutputPipeRetries(t *testing.T) {
	name := fmt.Sprintf("%stest-%d-%d", outputPipePrefix, os.Getpid(), time.Now().UnixNano())
	self, err := priv.CurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	sddl := "D:P(A;;GA;;;" + self + ")"
	got := make(chan error, 1)
	go func() {
		w, err := openOutputPipe(name, 5*time.Second)
		if err == nil {
			_, err = w.WriteString("late\n")
			w.Close()
		}
		got <- err
	}()
	time.Sleep(200 * time.Millisecond) // the client is retrying: no pipe yet
	p, err := listenOutput(name, sddl)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	done := make(chan struct{})
	go func() { p.copyTo(&b); close(done) }()
	if err := <-got; err != nil {
		t.Fatalf("client: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("copy did not end when the client closed")
	}
	if b.String() != "late\n" {
		t.Errorf("got %q", b.String())
	}
	// The name is taken while the pipe exists: a second server fails.
	p2, err := listenOutput(name+"-2", sddl)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := listenOutput(name+"-2", sddl); err == nil {
		t.Error("a second instance of the name was created")
	}
	// Busy: the one instance has a client; another gives up at its deadline.
	c1, err := openOutputPipe(name+"-2", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := openOutputPipe(name+"-2", 300*time.Millisecond); !errors.Is(err, windows.ERROR_PIPE_BUSY) {
		t.Errorf("busy pipe: %v", err)
	}
	if d := time.Since(start); d < 250*time.Millisecond || d > 3*time.Second {
		t.Errorf("gave up after %v", d)
	}
	c1.Close()
	p2.release()
	p2.copyTo(io.Discard)
	// Nothing there at all: a plain error after the deadline.
	if _, err := openOutputPipe(name+"-none", 100*time.Millisecond); err == nil {
		t.Error("opened a pipe that does not exist")
	}
}
