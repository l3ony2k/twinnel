package main

import (
	"runtime"
	"time"

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
//   - Windows 11 (Fluent 2): Segoe UI Variable (Text, Display for titles),
//     Cascadia Mono / Consolas for code (MyGo's monospace), 14 px body,
//     Mica behind the navigation pane under the system's own title bar,
//     cards of radius 8 and controls of 4, 40 px rows, WinUI's neutrals
//     and signal colors with the system's accent; the tray flyout on
//     Acrylic (Windows 11), opaque on Windows 10. Cross-compiled and
//     unit-tested here; HANDOFF of feat/windows-gui has the real-machine
//     plan.
//   - Linux (GNOME / libadwaita, KDE, Hyprland): the desktop's UI font
//     (gtk-font-name through fontconfig: Cantarell, Noto Sans, Inter…),
//     14 px body, no material (opaque sidebar), libadwaita's colors with
//     the portal's accent and color scheme, the title bar drawn by the app
//     with the desktop's window buttons over it (none where the compositor
//     decorates: KWin, Hyprland), boxed lists of radius 12 with 6 on
//     controls, 46 px rows; the tray icon's menu, and its activation (a
//     left click on KDE/quickshell, a double click on GNOME) opens the
//     panel as a window at the top right (panel_linux.go).
type platformStyle struct {
	// Window chrome.
	titleBar       mygo.TitleBarStyle
	sidebarMat     mygo.Vibrancy // material behind the sidebar; "" = opaque
	sidebarWidth   float32
	contentMax     float32 // widest a page's content grows
	pagePadX       float32
	pagePadTop     float32
	groupRadius    float32 // grouped boxes (cards)
	controlRadius  float32
	innerRadius    float32 // boxes inside a group: tiles, code, editors
	rowMinHeight   float32
	bodySize       float32 // body text
	smallSize      float32 // secondary text, captions
	calloutSize    float32 // notes, hints, table cells, code: one step under body
	leadSize       float32 // a verdict or state line that leads a card
	figureSize     float32 // a large number (the traffic rate)
	panelTextSize  float32 // the flyout's body text, as the system's menu bar extras
	titleSize      float32 // page titles
	headingSize    float32 // group headings
	titleWeight    int
	headingWeight  int
	panelPad       float32 // the flyout's padding
	panelWindow    bool    // the panel is a window placed by the app, not a flyout of the tray (Linux: AppIndicator tells no icon position)
	trafficLights  bool    // macOS's traffic lights sit in the title bar (sidebar.go measures from them)
	titleBarHeight int     // the title bar the window draws, where the system centers its buttons in it (Linux); 0 = the system's
	sidebarKey     ui.Key  // the sidebar toggle's key without modifiers (Linux: F9, as GNOME's apps); 0 = none
	adwaita        bool    // libadwaita's colors (Linux)
	fluent         bool    // WinUI's colors (Windows)
	// Type: the UI font ("" is the system's) and the one for large titles
	// (Segoe UI Variable's Text and Display optical sizes on Windows 11,
	// Segoe UI before it).
	uiFont, displayFont string
	// panelVibrancy is the flyout's material where it is not a popover
	// (Acrylic on Windows 11; Windows 10 draws the background instead).
	panelVibrancy mygo.Vibrancy
	// Shortcuts that differ by platform: moving a layer (⌥⌘↑ on macOS,
	// Ctrl+Alt+↑ on Linux, Alt+Shift+↑ on Windows, where Ctrl+Alt is
	// AltGr) and going back (⌘[ on macOS, Ctrl+[ on Linux, Alt+← on
	// Windows).
	reorderMods ui.Modifiers
	backMods    ui.Modifiers
	backKey     ui.Key
	// cliLink: 本地 Agent → 命令行工具 links the CLI into a bin folder (not on
	// Windows: install.ps1 puts the install's bin\ on the user's PATH).
	cliLink         bool
	trayTitle       bool    // the menu bar item shows text beside its icon (macOS)
	dock            bool    // a Dock icon by setting (macOS; elsewhere the taskbar follows the window)
	panelMaterialOK bool    // the flyout shows the popover material
	pushFace        bool    // plain buttons drawn as NSButton (pushFace)
	titleBarSize    float32 // the page title in the toolbar
	titleBarWeight  int
	navSel          navSelection // how the sidebar shows its selected row (nav.go)
	// menuDrop is how far below an element's bottom edge MyGo must anchor
	// its native menu for the menu to open just under it (menuBelow): on
	// macOS, AppKit's 5 pt inset above the first item plus a 4 pt gap,
	// as NSPopUpButton's pull-down keeps. 0 where the menu opens at the
	// point given.
	menuDrop float32
}

// navSelection is how a platform's sidebar marks the selected row.
type navSelection int

const (
	// navAccentFill: the row filled with the accent, icon and label in
	// AccentText, while the window is key (macOS 15 and earlier).
	navAccentFill navSelection = iota
	// navNeutral: a neutral translucent fill, the label in the text color
	// and the icon in the accent as on every row, no hover (macOS 26+
	// Finder; GNOME's navigation sidebar).
	navNeutral
	// navNeutralPill: navNeutral with a short accent pill at the row's
	// leading edge and a hover fill (Windows 11 NavigationView).
	navNeutralPill
)

var plat = platformFor(runtime.GOOS)

func platformFor(goos string) platformStyle {
	switch goos {
	case "windows":
		return platformStyle{
			titleBar: mygo.TitleBarDefault, sidebarMat: mygo.VibrancyMica,
			sidebarWidth: 240, contentMax: 1000, pagePadX: 32, pagePadTop: 24,
			groupRadius: 8, controlRadius: 4, innerRadius: 6, rowMinHeight: 40,
			bodySize: 14, smallSize: 12, calloutSize: 13, leadSize: 16, figureSize: 20, panelTextSize: 13, titleSize: 28, headingSize: 14,
			titleWeight: 600, headingWeight: 600, panelPad: 16,
			titleBarSize: 28, titleBarWeight: 600, // Windows 11 Settings: the page title stays large
			navSel: navNeutralPill, fluent: true,
			uiFont:        "Segoe UI Variable Text, Segoe UI",
			displayFont:   "Segoe UI Variable Display, Segoe UI",
			panelVibrancy: mygo.VibrancyAcrylic,
			reorderMods:   ui.Alt | ui.Shift,
			backMods:      ui.Alt, backKey: ui.KeyLeft,
		}
	case "linux":
		return platformStyle{
			titleBar: mygo.TitleBarHidden, titleBarHeight: 52, sidebarKey: ui.KeyF9, adwaita: true,
			sidebarWidth: 220, contentMax: 900, pagePadX: 24, pagePadTop: 24,
			groupRadius: 12, controlRadius: 6, innerRadius: 8, rowMinHeight: 46,
			bodySize: 14, smallSize: 12, calloutSize: 13, leadSize: 16, figureSize: 20, panelTextSize: 13, titleSize: 20, headingSize: 14,
			titleWeight: 700, headingWeight: 700, panelPad: 16, panelWindow: true,
			titleBarSize: 15, titleBarWeight: 700, navSel: navNeutral,
			reorderMods: ui.Alt | ui.Ctrl, backMods: ui.Ctrl, backKey: ui.KeyBracketLeft, cliLink: true,
		}
	}
	navSel := navAccentFill // macOS 15 and earlier
	if liquidGlass {
		navSel = navNeutral // macOS 26+: Finder's neutral selection
	}
	return platformStyle{ // darwin
		titleBar: mygo.TitleBarHiddenInset, sidebarMat: mygo.VibrancySidebar,
		sidebarWidth: 216, contentMax: 1080, pagePadX: 28, pagePadTop: 20,
		groupRadius: 10, controlRadius: 6, innerRadius: 8, rowMinHeight: 24,
		bodySize: 13, smallSize: 11, calloutSize: 12, leadSize: 15, figureSize: 20, panelTextSize: 13, titleSize: 20, headingSize: 13,
		titleWeight: 600, headingWeight: 600, panelPad: 14, panelMaterialOK: true,
		pushFace: true, trayTitle: true, dock: true, trafficLights: true,
		// A unified toolbar's window title (System Settings, Finder): about
		// 15 pt bold, not the 20 pt of an in-content heading.
		titleBarSize: 15, titleBarWeight: 700,
		navSel: navSel, menuDrop: menuInsetTop + menuGap,
		reorderMods: ui.Alt | ui.Super, backMods: ui.Super, backKey: ui.KeyBracketLeft, cliLink: true,
	}
}

// The native menu's geometry on macOS (measured on macOS 27 with
// NSMenu popUpMenuPositioningItem:nil atLocation:inView:, and against an
// NSPopUpButton pull-down): the menu's top edge is menuInsetTop above
// the point given, and a pull-down's menu starts a few points below its
// button.
const (
	menuInsetTop = 5
	menuGap      = 4
)

// systemTheme returns the app's theme for the appearance: the platform's
// own grays and status colors (macOS system colors on darwin), the user's
// accent and contrast settings on top (WithPreferences).
func systemTheme(dark bool, p ui.Preferences) *ui.Theme {
	var t *ui.Theme
	if plat.adwaita {
		t = adwaitaTheme(dark)
	} else if plat.fluent {
		t = fluentTheme(dark)
	} else if dark {
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
	return finishTheme(t, p)
}

// adwaitaTheme is libadwaita's palette (GNOME 47): the window and view
// grays, the blue accent, the status colors. The portal's accent replaces
// the blue where the desktop has one (WithPreferences).
func adwaitaTheme(dark bool) *ui.Theme {
	if dark {
		t := ui.DarkTheme()
		t.Background = ui.Hex("#222226")
		t.Surface = ui.Hex("#2e2e32")
		t.SurfaceHover = ui.Hex("#38383c")
		t.SurfacePressed = ui.Hex("#424246")
		t.Border = ui.RGBA(255, 255, 255, 0.15)
		t.Text = ui.Hex("#ffffff")
		t.TextMuted = ui.Hex("#a3a3a8")
		t.Danger = ui.Hex("#ff7b63")
		t.Warning = ui.Hex("#f8e45c")
		t.Success = ui.Hex("#8ff0a4")
		return t.WithAccent(ui.Hex("#3584e4"))
	}
	t := ui.LightTheme()
	t.Background = ui.Hex("#fafafb")
	t.Surface = ui.Hex("#ffffff")
	t.SurfaceHover = ui.Hex("#f0f0f2")
	t.SurfacePressed = ui.Hex("#e4e4e7")
	t.Border = ui.RGBA(0, 0, 6, 0.15)
	t.Text = ui.Hex("#2e2e32")
	t.TextMuted = ui.Hex("#6c6c72")
	t.Danger = ui.Hex("#c01c28")
	t.Warning = ui.Hex("#9c6e03")
	t.Success = ui.Hex("#1b8553")
	return t.WithAccent(ui.Hex("#3584e4"))
}

// fluentTheme is WinUI 3's palette (Windows 11): the Mica base, card and
// control fills, text and signal colors. The system's accent replaces the
// default blue (WithPreferences).
func fluentTheme(dark bool) *ui.Theme {
	if dark {
		t := ui.DarkTheme()
		t.Background = ui.Hex("#202020")
		t.Surface = ui.Hex("#2d2d2d")
		t.SurfaceHover = ui.Hex("#323232")
		t.SurfacePressed = ui.Hex("#272727")
		t.Border = ui.RGBA(255, 255, 255, 0.0837)
		t.Text = ui.Hex("#ffffff")
		t.TextMuted = ui.Hex("#c5c5c5")
		t.Danger = ui.Hex("#ff99a4")
		t.Warning = ui.Hex("#fce100")
		t.Success = ui.Hex("#6ccb5f")
		return t.WithAccent(ui.Hex("#60cdff"))
	}
	t := ui.LightTheme()
	t.Background = ui.Hex("#f3f3f3")
	t.Surface = ui.Hex("#fbfbfb")
	t.SurfaceHover = ui.Hex("#f6f6f6")
	t.SurfacePressed = ui.Hex("#f0f0f0")
	t.Border = ui.RGBA(0, 0, 0, 0.0803)
	t.Text = ui.Hex("#1a1a1a")
	t.TextMuted = ui.Hex("#5d5d5d")
	t.Danger = ui.Hex("#c42b1c")
	t.Warning = ui.Hex("#9d5d00")
	t.Success = ui.Hex("#0f7b0f")
	return t.WithAccent(ui.Hex("#005fb8"))
}

// finishTheme sets what every platform's theme shares.
func finishTheme(t *ui.Theme, p ui.Preferences) *ui.Theme {
	t.Radius = plat.controlRadius
	t.FontSize = plat.bodySize
	t.Font = plat.uiFont
	// Tooltips (labels) open after half a second, as AppKit's; once one
	// shows, its neighbours open at once (MyGo's warm-up).
	t.TooltipDelay = 500 * time.Millisecond
	return t.WithPreferences(p)
}

// displayFont sets the platform's font for large titles on e, where it
// has one (Segoe UI Variable Display on Windows 11).
func displayFont(e ui.Element) ui.Element {
	if plat.displayFont != "" {
		e.Font(plat.displayFont)
	}
	return e
}
