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
// for this process, then runs the install's install.ps1 -Uninstall
// (appRemovalScript), logging to %TEMP%\twinnel-remove.log. The PATH
// entry goes here too, through the same code as 设置 → 命令行工具, so a
// folder without install.ps1 loses it as well.
func removeAppBundle(dir string) {
	if _, err := userPath.Remove(winpath.Join(dir, "bin")); err != nil {
		log.Printf("remove app: PATH: %v", err)
	}
	script, err := appRemovalScript(os.Getpid(), dir, startMenuShortcut(), winpath.Join(os.TempDir(), "twinnel-remove.log"))
	if err != nil {
		log.Printf("remove app: %v", err)
		return
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	cmd.Dir = os.TempDir() // not the install folder, which is about to go
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

// hideConsole runs a console program (twinnel.exe) without a console
// window of its own: the app is a GUI program, so one would flash up.
func hideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	// `helper install|upgrade|uninstall` raises the UAC prompt from this
	// child: let it come to the front rather than flash in the taskbar.
	procAllowSetForeground.Call(asfwAny)
}

var procAllowSetForeground = user32.NewProc("AllowSetForegroundWindow")

const asfwAny = ^uintptr(0) // ASFW_ANY, (DWORD)-1
