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
	iconTun       = svgIcon("globe")
	iconFlush     = svgIcon("eraser")
	iconCollapsed = svgIcon("chevron-right")
	iconExpanded  = svgIcon("chevron-down")
	iconDuet      = svgIcon("duet")
	// Placeholders until the design pass draws their own icons.
	iconConfig   = svgIcon("list")
	iconHistory  = svgIcon("rotate-cw")
	iconKernel   = svgIcon("gauge")
	iconTakeover = svgIcon("log-out")
)

// trayIcon returns a menu bar template icon (32×32 PNG = 16 pt @2x, black
// on transparent): two interlocked rings whose shape tells the state
// (status.TrayIconFor picks it; never color, the icon is a template).
func trayIcon(v status.TrayIcon) []byte {
	b, err := icons.FS.ReadFile("tray/" + string(v) + ".png")
	if err != nil {
		panic(err)
	}
	return b
}
