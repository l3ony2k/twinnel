//go:build windows

package main

import (
	"golang.org/x/sys/windows"

	"github.com/l3ony2k/twinnel/assets/icons"
	"github.com/l3ony2k/twinnel/core/status"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	procGetDpiForSystem = user32.NewProc("GetDpiForSystem")
	procGetSysMetricDpi = user32.NewProc("GetSystemMetricsForDpi")
)

const smCxSmIcon = 49

// platformTrayIcon is the notification area's mark for v: the square
// black drawing at the small icon size of the system's DPI (the taskbar's),
// shown as a template, which MyGo draws white on a dark taskbar. The
// macOS mark (41×36) would be squashed: MyGo makes a square icon of a
// PNG's width.
func platformTrayIcon(v status.TrayIcon) []byte {
	b, err := icons.TrayWindows.ReadFile("tray/windows/" + string(v) + "-light.ico")
	if err != nil {
		return nil
	}
	img, err := icoPNG(b, systemTrayPx())
	if err != nil {
		return nil
	}
	return img
}

// systemTrayPx is SM_CXSMICON at the system DPI (Windows 10 1607+), else
// 16 px scaled by GetDpiForSystem, else 16.
func systemTrayPx() int {
	dpi := 96
	if procGetDpiForSystem.Find() == nil {
		if d, _, _ := procGetDpiForSystem.Call(); d > 0 {
			dpi = int(d)
		}
	}
	if procGetSysMetricDpi.Find() == nil {
		if px, _, _ := procGetSysMetricDpi.Call(smCxSmIcon, uintptr(dpi)); px > 0 {
			return int(px)
		}
	}
	return trayIconPx(dpi)
}
