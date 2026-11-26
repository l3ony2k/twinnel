package main

import (
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/assets/icons"
	"github.com/l3ony2k/duet/core/status"
)

// Line icons (assets/icons, Kubai line style, 24 grid, stroke 2). ui.Icon
// draws them in the text color.

func svgIcon(name string) *ui.SVG {
	b, err := icons.FS.ReadFile(name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(b)
}

var (
	iconOverview  = svgIcon("layout-dashboard")
	iconProxies   = svgIcon("split")
	iconTailscale = svgIcon("monitor-smartphone")
	iconHealth    = svgIcon("heart-pulse")
	iconConfig    = svgIcon("layers")
	iconHistory   = svgIcon("history")
	iconKernel    = svgIcon("cpu")
	iconTakeover  = svgIcon("import")
	iconLogs      = svgIcon("list")
	iconSettings  = svgIcon("settings")

	iconRefresh   = svgIcon("rotate-cw")
	iconCopy      = svgIcon("copy")
	iconSpeed     = svgIcon("gauge")
	iconExitNode  = svgIcon("log-out")
	iconWarning   = svgIcon("triangle-alert")
	iconOK        = svgIcon("circle-check")
	iconError     = svgIcon("circle-x")
	iconSkip      = svgIcon("circle-minus")
	iconPower     = svgIcon("power")
	iconTun       = svgIcon("tunnel")
	iconGlobe     = svgIcon("globe")
	iconFlush     = svgIcon("eraser")
	iconCollapsed = svgIcon("chevron-right")
	iconExpanded  = svgIcon("chevron-down")
	iconDuet      = svgIcon("duet")

	iconSubscription = svgIcon("rss")
	iconLayer        = svgIcon("square-pen")
	iconManaged      = svgIcon("file-lock")
	iconRollback     = svgIcon("undo-2")
	iconService      = svgIcon("server")
	iconGeo          = svgIcon("map")
	iconDashboard    = svgIcon("app-window")
	iconSecure       = svgIcon("key-round")
	iconLeakGuard    = svgIcon("shield-droplet")
	iconPathDirect   = svgIcon("nodes-direct")
	iconPathRelay    = svgIcon("nodes-relay")
)

// trayIcon returns a menu bar template icon (41×36 PNG = 18 pt tall @2x,
// black on transparent; macOS scales it on 1x screens): two interlocked rings whose shape tells the state
// (status.TrayIconFor picks it; never color, the icon is a template).
func trayIcon(v status.TrayIcon) []byte {
	b, err := icons.FS.ReadFile("tray/" + string(v) + ".png")
	if err != nil {
		panic(err)
	}
	return b
}
