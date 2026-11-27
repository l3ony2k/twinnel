//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The test binary copied into the install stands in for an agent's
// `twinnel mcp`: started with holdEnv, it keeps running (and its file
// open) until it is stopped.
const holdEnv = "TWINNEL_TEST_HOLD"

func init() {
	if os.Getenv(holdEnv) == "1" {
		time.Sleep(10 * time.Minute)
		os.Exit(0)
	}
}

// 完全移除 on Windows, for real: the script appRemovalScript writes runs
// under powershell.exe as removeAppBundle starts it, waits for the app,
// and runs the install's own install.ps1 -Uninstall (the one in
// packaging/windows), which stops the program still running from the
// folder and removes it. A folder without install.ps1 is removed by the
// script itself, with the shortcut. Nothing outside temp dirs changes; it
// refuses to run on a machine with Twinnel installed (install.ps1's
// uninstall would take its shortcut and start-at-login entry).
func TestAppRemovalRunsInstallScriptWindows(t *testing.T) {
	ps, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("no powershell.exe")
	}
	if why := twinnelOnMachine(); why != "" {
		t.Skipf("Twinnel is on this machine (%s)", why)
	}
	installPS1, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "install.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	selfBytes, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}

	// The app this script waits for: already gone.
	gone := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", "exit 0")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	appPid := gone.ProcessState.Pid()

	t.Run("install.ps1", func(t *testing.T) {
		root := longPathName(t, t.TempDir())
		dir := filepath.Join(root, "Twinnel")
		put(t, filepath.Join(dir, installMarker), "v")
		put(t, filepath.Join(dir, "install.ps1"), string(installPS1))
		put(t, filepath.Join(dir, "Twinnel.exe"), "app")
		put(t, filepath.Join(dir, "bin", "twinnel.exe"), string(selfBytes))
		mcp := exec.Command(filepath.Join(dir, "bin", "twinnel.exe"))
		mcp.Env = append(os.Environ(), holdEnv+"=1")
		mcp.Dir = root
		if err := mcp.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _ = mcp.Wait(); close(done) }()
		t.Cleanup(func() { _ = mcp.Process.Kill(); <-done })

		log := filepath.Join(root, "remove.log")
		runRemoval(t, ps, appPid, dir, filepath.Join(root, "unused.lnk"), log)
		out := readLog(t, log)
		t.Logf("install.ps1 -Uninstall:\n%s", out)
		if !strings.Contains(out, "Twinnel removed.") || strings.Contains(strings.ToLower(out), "exception") {
			t.Errorf("log:\n%s", out)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s still there: %v", dir, err)
		}
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the program running from the install was not stopped")
		}
	})

	t.Run("no install.ps1", func(t *testing.T) {
		root := longPathName(t, t.TempDir())
		dir := filepath.Join(root, "Twinnel")
		put(t, filepath.Join(dir, installMarker), "v")
		put(t, filepath.Join(dir, "Twinnel.exe"), "app")
		lnk := filepath.Join(root, "Twinnel.lnk")
		put(t, lnk, "shortcut")
		runRemoval(t, ps, appPid, dir, lnk, "")
		for _, p := range []string{dir, lnk} {
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Errorf("%s still there: %v", p, err)
			}
		}
	})
}

// runRemoval runs appRemovalScript the way removeAppBundle starts it, and
// waits for it.
func runRemoval(t *testing.T, ps string, pid int, dir, lnk, log string) {
	t.Helper()
	script, err := appRemovalScript(pid, dir, lnk, log)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	cmd.Dir = os.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("removal script: %v\n%s", err, out)
	}
}

// twinnelOnMachine names what of a real install install.ps1 -Uninstall
// would touch besides its folder, "" when there is none.
func twinnelOnMachine() string {
	if p := startMenuShortcut(); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("ProgramFiles"), `Twinnel\twinnel-helper.exe`)); err == nil {
		return "the helper"
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if _, _, err := k.GetStringValue("io.github.l3ony2k.twinnel"); err == nil {
			return "the start-at-login entry"
		}
	}
	return ""
}

// readLog reads the removal log: UTF-16 with a BOM from Windows
// PowerShell's *>, UTF-8 from PowerShell 7.
func readLog(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe {
		u := make([]uint16, (len(b)-2)/2)
		for i := range u {
			u[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
		}
		return windows.UTF16ToString(u)
	}
	return string(b)
}

func put(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// longPathName: p without 8.3 names, as Windows reports a running
// program's path (install.ps1 matches processes by it).
func longPathName(t *testing.T, p string) string {
	t.Helper()
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetLongPathName(u, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		t.Fatalf("long path of %s: %v", p, err)
	}
	return windows.UTF16ToString(buf[:n])
}
