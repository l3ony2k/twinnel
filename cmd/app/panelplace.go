package main

import "strings"

// Where the Linux panel window goes (panel_linux.go). AppIndicator tells
// neither where the tray icon is nor where it was clicked, so the panel
// goes to a corner of the work area: the top right by default, under the
// top bar of GNOME and of most Hyprland setups (quickshell, waybar). KDE's
// default panel is at the bottom: bottom-right there (panelDefault).
// TWINNEL_PANEL_POSITION picks another place: top-left, top-right,
// bottom-left, bottom-right, center, or pointer (X11 only: where the
// pointer is, which is the icon's place after a click on it).

type rect struct{ x, y, w, h int }

// panelMargin is the room between the panel and the work area's edges.
const panelMargin = 8

// panelDefault is the position for $XDG_CURRENT_DESKTOP when
// TWINNEL_PANEL_POSITION is not set.
func panelDefault(currentDesktop string) string {
	for _, d := range strings.Split(currentDesktop, ":") {
		if strings.EqualFold(d, "KDE") {
			return "bottom-right"
		}
	}
	return "top-right"
}

// panelOrigin is the panel's top-left corner for a w×h panel in the work
// area wa, by the position setting pos and the pointer (ptrOK false where
// it cannot be read: Wayland). An unknown or empty pos is top-right;
// pointer without a pointer is top-right too.
func panelOrigin(wa rect, w, h int, pos string, px, py int, ptrOK bool) (int, int) {
	left := wa.x + panelMargin
	right := wa.x + wa.w - w - panelMargin
	top := wa.y + panelMargin
	bottom := wa.y + wa.h - h - panelMargin
	clampX := func(x int) int { return max(left, min(x, right)) }
	clampY := func(y int) int { return max(top, min(y, bottom)) }
	switch strings.ToLower(strings.TrimSpace(pos)) {
	case "top-left":
		return left, top
	case "bottom-left":
		return left, max(top, bottom)
	case "bottom-right":
		return max(left, right), max(top, bottom)
	case "center":
		return wa.x + (wa.w-w)/2, wa.y + (wa.h-h)/2
	case "pointer":
		if ptrOK {
			x := clampX(px - w/2)
			if py < wa.y+wa.h/2 { // a bar at the top: below the pointer
				return x, clampY(py + panelMargin)
			}
			return x, clampY(py - h - panelMargin) // a bar at the bottom: above it
		}
	}
	return max(left, right), top
}
