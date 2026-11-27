package main

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The Linux look: no macOS assumptions (traffic lights, materials, Dock,
// a title beside the tray icon), the app's own title bar with the
// desktop's buttons centered in 52 px, the panel as a placed window.
func TestLinuxPlatformStyle(t *testing.T) {
	p := platformFor("linux")
	switch {
	case p.trafficLights, p.dock, p.trayTitle, p.panelMaterialOK, p.pushFace:
		t.Errorf("linux style keeps a macOS feature: %+v", p)
	case p.sidebarMat != mygo.VibrancyNone && p.sidebarMat != "":
		t.Errorf("linux sidebar material %q", p.sidebarMat)
	case p.titleBar != mygo.TitleBarHidden || p.titleBarHeight != 52:
		t.Errorf("linux title bar %q %d", p.titleBar, p.titleBarHeight)
	case !p.panelWindow || p.sidebarKey != ui.KeyF9 || !p.adwaita:
		t.Errorf("linux panel/sidebar key/palette: %+v", p)
	}
	if d := platformFor("darwin"); !d.trafficLights || d.panelWindow || d.adwaita {
		t.Errorf("darwin style changed: %+v", d)
	}
	if w := platformFor("windows"); w.trafficLights || w.panelWindow || w.adwaita {
		t.Errorf("windows style changed: %+v", w)
	}
}

func TestAdwaitaTheme(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := adwaitaTheme(dark)
		if th.Dark != dark || th.Background == th.Text {
			t.Errorf("adwaita dark=%v: %+v", dark, th)
		}
	}
}
