package main

import (
	"testing"

	"github.com/l3ony2k/duet/core/status"
)

func TestSideSnap(t *testing.T) {
	for _, tc := range []struct {
		raw   float32
		mode  sidebarMode
		width float32
	}{
		{-40, sideHidden, 200},
		{sideRailW/2 - 1, sideHidden, 200},
		{sideRailW / 2, sideRail, 200},
		{(sideRailW+sideMinW)/2 - 1, sideRail, 200},
		{(sideRailW + sideMinW) / 2, sideExpanded, sideMinW},
		{250, sideExpanded, 250},
		{900, sideExpanded, sideMaxW},
	} {
		m, w := sideSnap(tc.raw, 200, sideMinW)
		if m != tc.mode || w != tc.width {
			t.Errorf("sideSnap(%v) = %v, %v; want %v, %v", tc.raw, m, w, tc.mode, tc.width)
		}
	}
}

// The labels' measured width raises the narrowest and the default
// expanded width, never above sideMaxW, and the snapping follows it.
func TestSidebarNeedRaisesWidths(t *testing.T) {
	s := sidebarState{need: 260}
	if s.minW() != 260 || s.expandedW() != 260 {
		t.Errorf("need 260: min %v default %v", s.minW(), s.expandedW())
	}
	s.width = 200 // dragged narrower in a language that needed less
	if s.expandedW() != 260 {
		t.Errorf("dragged 200, need 260: %v", s.expandedW())
	}
	if m, w := sideSnap(250, 200, s.minW()); m != sideExpanded || w != 260 {
		t.Errorf("snap 250 with min 260: %v %v", m, w)
	}
	s.need = 9000
	if s.minW() != sideMaxW {
		t.Errorf("need 9000: min %v", s.minW())
	}
}

func TestSidebarCycleAndConfig(t *testing.T) {
	var s sidebarState
	s.load(status.Config{})
	if s.mode != sideExpanded || s.expandedW() != plat.sidebarWidth {
		t.Fatalf("default: mode %v width %v", s.mode, s.expandedW())
	}
	want := []sidebarMode{sideRail, sideHidden, sideExpanded}
	for _, w := range want {
		s.setMode(s.mode.next())
		if s.mode != w {
			t.Fatalf("cycle: got %v, want %v", s.mode, w)
		}
	}
	// A sidebar hidden from the rail keeps the rail laid out (it slides
	// out as it was), and comes back expanded.
	s.setMode(sideRail)
	s.setMode(sideHidden)
	if s.layoutMode() != sideRail || s.widthOf(s.mode) != 0 {
		t.Errorf("hidden from the rail: layout %v width %v", s.layoutMode(), s.widthOf(s.mode))
	}
	for _, tc := range []struct {
		cfg   status.Config
		mode  sidebarMode
		width float32
	}{
		{status.Config{Sidebar: "icons", SidebarWidth: 260}, sideRail, 260},
		{status.Config{Sidebar: "hidden", SidebarWidth: 10}, sideHidden, sideMinW},
		{status.Config{Sidebar: "bogus", SidebarWidth: 9000}, sideExpanded, sideMaxW},
	} {
		var s sidebarState
		s.load(tc.cfg)
		if s.mode != tc.mode || s.expandedW() != tc.width {
			t.Errorf("load %+v: %v %v", tc.cfg, s.mode, s.expandedW())
		}
		if tc.cfg.Sidebar != "bogus" && s.mode.configValue() != tc.cfg.Sidebar {
			t.Errorf("configValue %v = %q", s.mode, s.mode.configValue())
		}
	}
}
