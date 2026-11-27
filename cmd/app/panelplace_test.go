package main

import "testing"

func TestPanelOrigin(t *testing.T) {
	wa := rect{0, 30, 1920, 1050} // a 30 px bar at the top
	const w, h = 340, 400
	for _, c := range []struct {
		pos      string
		px, py   int
		ptrOK    bool
		wantX, y int
	}{
		{"", 0, 0, false, 1920 - 340 - 8, 38},
		{"bogus", 0, 0, false, 1572, 38},
		{"top-left", 0, 0, false, 8, 38},
		{"bottom-right", 0, 0, false, 1572, 30 + 1050 - 400 - 8},
		{"bottom-left", 0, 0, false, 8, 672},
		{"center", 0, 0, false, 790, 355},
		{"pointer", 0, 0, false, 1572, 38},            // Wayland: no pointer
		{"pointer", 1900, 15, true, 1572, 38},         // icon at the top right
		{"pointer", 500, 1075, true, 330, 1075 - 408}, // a bottom bar: above the pointer
		{"pointer", 5, 40, true, 8, 48},
	} {
		x, y := panelOrigin(wa, w, h, c.pos, c.px, c.py, c.ptrOK)
		if x != c.wantX || y != c.y {
			t.Errorf("%q at (%d,%d) ptr %v: got (%d,%d), want (%d,%d)", c.pos, c.px, c.py, c.ptrOK, x, y, c.wantX, c.y)
		}
	}
}

func TestPanelDefault(t *testing.T) {
	if panelDefault("KDE") != "bottom-right" || panelDefault("Hyprland") != "top-right" || panelDefault("ubuntu:GNOME") != "top-right" {
		t.Error("panelDefault")
	}
}
