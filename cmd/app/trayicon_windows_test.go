//go:build windows

package main

import (
	"bytes"
	"image/png"
	"os/exec"
	"testing"

	"github.com/l3ony2k/twinnel/core/status"
)

// On a real Windows: the system's small icon size, and a square mark of
// that size for every state (MyGo makes a square HICON of a PNG's width).
func TestWindowsPlatformTrayIcon(t *testing.T) {
	px := systemTrayPx()
	if px < 16 || px > 64 {
		t.Fatalf("small icon size %d", px)
	}
	for _, st := range []status.TrayIcon{status.TrayOK, status.TrayWarn, status.TrayError, status.TrayUnknown, status.TrayTSDown, status.TrayMode, status.TrayNoHelper} {
		b := trayIcon(st)
		cfg, err := png.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Width != cfg.Height {
			t.Errorf("%s: %d×%d, want square", st, cfg.Width, cfg.Height)
		}
	}
}

func TestWindowsHideConsole(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit 0")
	hideConsole(cmd)
	if !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatal("console not hidden")
	}
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}
