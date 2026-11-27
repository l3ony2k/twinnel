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
	iconAbout     = svgIcon("info")

	iconRefresh   = svgIcon("rotate-cw")
	iconCopy      = svgIcon("copy")
	iconSpeed     = svgIcon("gauge")
	iconPreview   = svgIcon("file-diff") // 预览: what an apply would change
	iconWarning   = svgIcon("triangle-alert")
	iconOK        = svgIcon("circle-check")
	iconError     = svgIcon("circle-x")
	iconSkip      = svgIcon("circle-minus")
	iconTun       = svgIcon("tunnel")
	iconCollapsed = svgIcon("chevron-right")
	iconDuet      = svgIcon("duet")

	iconManaged    = svgIcon("file-lock")
	iconRollback   = svgIcon("undo-2")
	iconService    = svgIcon("server")
	iconGeo        = svgIcon("map")
	iconDashboard  = svgIcon("app-window")
	iconSecure     = svgIcon("key-round")
	iconLock       = svgIcon("lock") // a control that needs the helper
	iconLeakGuard  = svgIcon("shield-droplet")
	iconPathDirect = svgIcon("nodes-direct")
	iconPathRelay  = svgIcon("nodes-relay")
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
