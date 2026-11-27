package main

import "testing"

// The sidebar's selection follows each platform's own rule: neutral on
// macOS 26+ (Finder) and GNOME, a neutral fill with an accent pill on
// Windows 11, the accent fill only before macOS 26.
func TestNavSelectionPerPlatform(t *testing.T) {
	if got := platformFor("windows").navSel; got != navNeutralPill {
		t.Errorf("windows: %v, want navNeutralPill", got)
	}
	if got := platformFor("linux").navSel; got != navNeutral {
		t.Errorf("linux: %v, want navNeutral", got)
	}
	want := navAccentFill
	if liquidGlass {
		want = navNeutral
	}
	if got := platformFor("darwin").navSel; got != want {
		t.Errorf("darwin (liquidGlass=%v): %v, want %v", liquidGlass, got, want)
	}
}
