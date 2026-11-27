package main

import "testing"

// The page column is centered in a pane wider than it and its padding,
// and keeps the plain padding below that.
func TestColumnInset(t *testing.T) {
	pad := plat.pagePadX
	for _, tc := range []struct{ pane, max, want float32 }{
		{0, 1000, pad},                          // before the first layout
		{600, 1000, pad},                        // narrower than the column
		{1000 + 2*pad, 1000, pad},               // exactly fits
		{1400 + 2*pad, 1000, pad + 200},         // slack split in two
		{3240, 1280, pad + (3240-2*pad-1280)/2}, // full screen
	} {
		got := columnInset(tc.pane, tc.max)
		if got != tc.want {
			t.Errorf("columnInset(%v, %v) = %v, want %v", tc.pane, tc.max, got, tc.want)
		}
		if tc.pane > 2*pad+tc.max {
			if l, r := got, tc.pane-got-tc.max; l != r {
				t.Errorf("pane %v: left %v != right %v", tc.pane, l, r)
			}
		}
	}
}
