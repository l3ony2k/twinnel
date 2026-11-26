package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// ↑ and ↓ move a row of the tile grid and stop at its top and bottom.
func TestTileStep(t *testing.T) {
	// 7 tiles in 3 columns:  0 1 2 / 3 4 5 / 6
	for _, tc := range []struct {
		i        int
		up, down bool
		want     int
		ok       bool
	}{
		{4, true, false, 1, true},
		{4, false, true, 0, false}, // 7 is past the end: stays
		{3, false, true, 6, true},
		{1, true, false, 0, false}, // top row
		{6, true, false, 3, true},
		{2, false, true, 5, true},
		{5, false, false, 0, false},
	} {
		got, ok := tileStep(tc.i, 3, 7, tc.up, tc.down)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("tileStep(%d, up %v, down %v) = %d %v, want %d %v", tc.i, tc.up, tc.down, got, ok, tc.want, tc.ok)
		}
	}
	if got, ok := tileStep(0, 0, 3, false, true); !ok || got != 1 { // before the first layout: one column
		t.Errorf("cols 0: %d %v", got, ok)
	}
}

// The sidebar's first nine pages get ⌘1…⌘9 in its order; 设置 has ⌘,.
func TestNavSectionsDigits(t *testing.T) {
	want := []string{pageOverview, pageProxies, pageTailscale, pageHealth, pageConfig, pageHistory, pageKernel, pageTakeover, pageLogs}
	n := 0
	for _, sec := range navSections(0, 0, toneNeutral) {
		for _, it := range sec.items {
			if it.id == pageSettings {
				if it.digit != 0 {
					t.Errorf("settings has ⌘%d", it.digit)
				}
				continue
			}
			if n >= len(want) || it.id != want[n] || it.digit != n+1 || it.key != digitKeys[n] {
				t.Errorf("row %s: digit %d key %v, want %s ⌘%d", it.id, it.digit, it.key, want[min(n, len(want)-1)], n+1)
			}
			n++
		}
	}
	if n != len(want) {
		t.Errorf("%d numbered pages, want %d", n, len(want))
	}
	if digitKeys[0] != ui.Key1 || digitKeys[8] != ui.Key9 {
		t.Error("digit keys")
	}
}
