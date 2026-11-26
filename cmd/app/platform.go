package main

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The platform layer: what the app's look takes from the desktop it runs
// on. Views never test runtime.GOOS for looks; they read plat and the
// palette (style.go). Only macOS is built and checked today; the Windows
// and Linux values are the starting point for those ports:
//
//   - macOS (Apple HIG): system font (SF Pro + PingFang SC), 13 pt body,
//     sidebar of the window's "sidebar" material under a hidden-inset title
//     bar with the traffic lights over it, grouped boxes like System
//     Settings (radius 10), the user's accent color, the flyout as a
//     popover over the menu bar's own material.
//   - Windows 11 (Fluent 2): Segoe UI Variable, 14 px body, Mica behind the
//     navigation pane and the title bar drawn by the app (TitleBarHidden,
//     caption buttons by the system), cards of radius 8 and controls of 4,
//     40 px rows, the accent from the system; the tray flyout opaque with
//     the Acrylic material when available.
//   - Linux (GNOME / libadwaita): Cantarell or the desktop's font, 14 px
//     body, no material (opaque sidebar of the "view" color), the system's
//     own title bar, boxed lists of radius 12 with 6 on controls, 46 px
//     rows, the tray as a plain menu (panel.go: no flyout on Linux).
type platformStyle struct {
	// Window chrome.
	titleBar        mygo.TitleBarStyle
	sidebarMat      mygo.Vibrancy // material behind the sidebar; "" = opaque
	sidebarWidth    float32
	contentMax      float32 // widest a page's content grows
	pagePadX        float32
	pagePadTop      float32
	groupRadius     float32 // grouped boxes (cards)
	controlRadius   float32
	rowMinHeight    float32
	bodySize        float32 // body text
	smallSize       float32 // secondary text, captions
	titleSize       float32 // page titles
	headingSize     float32 // group headings
	titleWeight     int
	headingWeight   int
	panelPad        float32 // the flyout's padding
	trayMenuOnly    bool    // the tray opens a menu, not a flyout
	panelMaterialOK bool    // the flyout shows the popover material
}

var plat = platformFor(runtime.GOOS)

func platformFor(goos string) platformStyle {
	switch goos {
	case "windows":
		return platformStyle{
			titleBar: mygo.TitleBarDefault, sidebarMat: mygo.VibrancyMica,
			sidebarWidth: 240, contentMax: 1000, pagePadX: 32, pagePadTop: 24,
			groupRadius: 8, controlRadius: 4, rowMinHeight: 40,
			bodySize: 14, smallSize: 12, titleSize: 28, headingSize: 14,
			titleWeight: 600, headingWeight: 600, panelPad: 16,
		}
	case "linux":
		return platformStyle{
			titleBar:     mygo.TitleBarDefault,
			sidebarWidth: 220, contentMax: 900, pagePadX: 24, pagePadTop: 24,
			groupRadius: 12, controlRadius: 6, rowMinHeight: 46,
			bodySize: 14, smallSize: 12, titleSize: 20, headingSize: 14,
			titleWeight: 700, headingWeight: 700, panelPad: 16, trayMenuOnly: true,
		}
	}
	return platformStyle{ // darwin
		titleBar: mygo.TitleBarHiddenInset, sidebarMat: mygo.VibrancySidebar,
		sidebarWidth: 216, contentMax: 920, pagePadX: 28, pagePadTop: 20,
		groupRadius: 10, controlRadius: 6, rowMinHeight: 24,
		bodySize: 13, smallSize: 11, titleSize: 20, headingSize: 13,
		titleWeight: 600, headingWeight: 600, panelPad: 14, panelMaterialOK: true,
	}
}

// systemTheme returns the app's theme for the appearance: the platform's
// own grays and status colors (macOS system colors on darwin), the user's
// accent and contrast settings on top (WithPreferences).
func systemTheme(dark bool, p ui.Preferences) *ui.Theme {
	var t *ui.Theme
	if dark {
		t = ui.DarkTheme()
		t.Background = ui.Hex("#1e1e1e")
		t.Surface = ui.Hex("#3a3a3c")
		t.SurfaceHover = ui.Hex("#444446")
		t.SurfacePressed = ui.Hex("#4e4e50")
		t.Border = ui.RGBA(255, 255, 255, 0.12)
		t.Text = ui.Hex("#e8e8ea")
		t.TextMuted = ui.Hex("#98989d")
		t.Danger = ui.Hex("#ff453a")
		t.Warning = ui.Hex("#ff9f0a")
		t.Success = ui.Hex("#30d158")
		t = t.WithAccent(ui.Hex("#0a84ff"))
	} else {
		t = ui.LightTheme()
		t.Background = ui.Hex("#f5f5f5")
		t.Surface = ui.Hex("#ffffff")
		t.SurfaceHover = ui.Hex("#f2f2f2")
		t.SurfacePressed = ui.Hex("#e5e5e5")
		t.Border = ui.RGBA(0, 0, 0, 0.14)
		t.Text = ui.Hex("#1d1d1f")
		t.TextMuted = ui.Hex("#6e6e73")
		t.Danger = ui.Hex("#ff3b30")
		t.Warning = ui.Hex("#ff9500")
		t.Success = ui.Hex("#34c759")
		t = t.WithAccent(ui.Hex("#007aff"))
	}
	t.Radius = plat.controlRadius
	t.FontSize = plat.bodySize
	return t.WithPreferences(p)
}
