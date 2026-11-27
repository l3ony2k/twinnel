//go:build windows

package icons

import "embed"

// TrayWindows holds the notification area marks, tray/windows/<state>-light.ico
// (black, 16–48 px): Windows builds only. The app passes one size of them as
// a template image, which MyGo draws white on a dark taskbar.
//
//go:embed tray/windows/*-light.ico
var TrayWindows embed.FS
