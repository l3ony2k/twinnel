//go:build windows

package main

// UAC elevation. A normal user's `twinnel helper install|upgrade|uninstall`
// re-runs Twinnel elevated with ShellExecuteExW(verb "runas"): Windows shows
// its consent (or credentials) prompt once. The arguments are one command
// line quoted with scm.CommandLine (the C runtime's rules); no shell is
// involved. The elevated copy's console is hidden; it writes its output to
// a named pipe this copy serves (--output), never to a file a user could
// redirect.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/l3ony2k/twinnel/core/priv"
	"github.com/l3ony2k/twinnel/platform/windows/scm"
)

// platformEUID: 0 when the process is elevated (or LocalSystem), like
// root on Unix.
func platformEUID() int {
	if priv.Elevated() {
		return 0
	}
	return 1000
}

// outputPipePrefix is the only kind of --output the elevated copy accepts.
const outputPipePrefix = `\\.\pipe\twinnel-elevated-`

// Replaceable in tests: nothing in a test may show a UAC prompt.
var shellExecuteRunas = runasWait

func elevate(op string, f *installFlags, exe string, rargs []string) error {
	// The pipe for the installing user: --allow-sid is taken here, before
	// elevation, because an over-the-shoulder prompt runs the elevated copy
	// as another account.
	if op == "install" || op == "upgrade" {
		sid, err := priv.CurrentUserSID()
		if err != nil {
			return err
		}
		rargs = append(rargs, "--allow-sid", sid)
		for _, s := range allowSIDs {
			if s != sid {
				rargs = append(rargs, "--allow-sid", s)
			}
		}
	}
	var nonce [8]byte
	rand.Read(nonce[:])
	pipe := fmt.Sprintf("%s%d-%s", outputPipePrefix, os.Getpid(), hex.EncodeToString(nonce[:]))
	rargs = append(rargs, "--output", pipe)
	params, err := scm.CommandLine(rargs)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "# Windows will ask for administrator approval (UAC), then run:")
	fmt.Fprintf(stdout, "#   %s %s\n", scm.EscapeArg(exe), params)
	if !f.yes && !confirm() {
		return errors.New("cancelled; nothing was changed")
	}
	self, err := priv.CurrentUserSID()
	if err != nil {
		return err
	}
	// LocalSystem, Administrators (the elevated copy, whichever admin
	// account approved the prompt) and this user.
	sddl := "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + self + ")"
	// The pipe's instance exists before the elevated copy starts, so the
	// copy can connect at once (outpipe_windows.go).
	out, err := listenOutput(pipe, sddl)
	if err != nil {
		return fmt.Errorf("output pipe: %w", err)
	}
	var tail lastLine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		out.copyTo(io.MultiWriter(stdout, &tail))
	}()
	code, err := shellExecuteRunas(exe, params)
	out.release()
	waitTimeout(&wg, 5*time.Second)
	switch {
	case errors.Is(err, windows.ERROR_CANCELLED):
		return errors.New("cancelled; nothing was changed")
	case err != nil:
		return fmt.Errorf("helper %s: %w", op, err)
	case code != 0:
		if msg := tail.twinnelError(); msg != "" {
			return errors.New(msg)
		}
		return fmt.Errorf("helper %s failed in the elevated run (exit %d; see above)", op, code)
	}
	return nil
}

func waitTimeout(wg *sync.WaitGroup, d time.Duration) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
	}
}

// lastLine keeps the end of the elevated copy's output to report its
// error ("twinnel: …", printed by main) as this copy's.
type lastLine struct {
	mu  sync.Mutex
	buf []byte
}

func (l *lastLine) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, b...)
	if len(l.buf) > 8<<10 {
		l.buf = l.buf[len(l.buf)-8<<10:]
	}
	return len(b), nil
}

func (l *lastLine) twinnelError() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	text := strings.ReplaceAll(string(l.buf), "\r", "")
	i := strings.LastIndex(text, "twinnel: ")
	if i < 0 {
		return ""
	}
	text = text[i+len("twinnel: "):]
	if j := strings.IndexByte(text, '\n'); j >= 0 {
		text = text[:j]
	}
	return strings.TrimSpace(text)
}

// attachOutput sends the elevated copy's output (stdout and the errors
// main prints) to the pipe of the copy that asked for elevation. Only that
// kind of pipe is accepted: an elevated process never opens a path a user
// chose for writing.
func attachOutput(path string) error {
	if path == "" {
		return nil
	}
	if !strings.HasPrefix(path, outputPipePrefix) || strings.ContainsAny(path[len(outputPipePrefix):], `\/:`) {
		return fmt.Errorf("--output %q is not an output pipe", path)
	}
	w, err := openOutputPipe(path, 5*time.Second)
	if err != nil {
		// The run itself does not depend on it: its output then stays in
		// its hidden console, and the exit code still reaches the caller.
		return nil
	}
	stdout, os.Stdout, os.Stderr = w, w, w
	return nil
}

// shellExecuteInfo is SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	cbSize         uint32
	fMask          uint32
	hwnd           windows.Handle
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       windows.Handle
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      windows.Handle
	dwHotKey       uint32
	hIconOrMonitor windows.Handle
	hProcess       windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskFlagNoUI       = 0x00000400
	seeMaskNoConsole      = 0x00008000
	swHide                = 0
)

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// runasWait starts exe elevated and waits for it; it returns its exit
// code, or ERROR_CANCELLED when the prompt was dismissed.
func runasWait(exe, params string) (uint32, error) {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	args, err := windows.UTF16PtrFromString(params)
	if err != nil {
		return 0, err
	}
	info := shellExecuteInfo{
		fMask:  seeMaskNoCloseProcess | seeMaskFlagNoUI | seeMaskNoConsole,
		lpVerb: verb, lpFile: file, lpParameters: args, nShow: swHide,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0, callErr
	}
	if info.hProcess == 0 {
		return 0, errors.New("ShellExecuteEx returned no process")
	}
	defer windows.CloseHandle(info.hProcess)
	if _, err := windows.WaitForSingleObject(info.hProcess, windows.INFINITE); err != nil {
		return 0, err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return 0, err
	}
	return code, nil
}
