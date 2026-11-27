//go:build windows

package main

import (
	"log"
	"os"
	"os/exec"
	"syscall"

	"github.com/l3ony2k/twinnel/platform/windows/winpath"
)

// exeName is a program's file name: name.exe.
func exeName(name string) string { return name + ".exe" }

// helperInstalledPaths: the installed twinnel-helper.exe (the service
// itself is a registry key the SCM owns, not a file).
var helperInstalledPaths = []string{winpath.HelperBin()}

func init() {
	// No links into a bin folder: install.ps1 puts the install's bin\ on
	// the user's PATH, and the agent snippets name bin\twinnel.exe.
	cliTargets = func() []string { return nil }
}

// removeAppBundle deletes the install folder once the app has quit (a
// running .exe cannot be deleted or recycled): a hidden PowerShell waits
// for this process, then removes the folder, install.ps1's Start menu
// shortcut and its PATH entry (appRemovalScript).
func removeAppBundle(dir string) {
	script, err := appRemovalScript(os.Getpid(), dir, startMenuShortcut())
	if err != nil {
		log.Printf("remove app: %v", err)
		return
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := cmd.Start(); err != nil {
		log.Printf("remove app: %v", err)
		return
	}
	_ = cmd.Process.Release()
}

const createNoWindow = 0x08000000

// startMenuShortcut is install.ps1's shortcut.
func startMenuShortcut() string {
	if d := os.Getenv("APPDATA"); d != "" {
		return winpath.Join(d, `Microsoft\Windows\Start Menu\Programs\Twinnel.lnk`)
	}
	return ""
}
