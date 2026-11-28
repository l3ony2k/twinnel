package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Node names lose their flags only where the fonts can't draw them: never
// on macOS, always on Windows, and on Linux as the measurement says.
func TestNodeTextFlags(t *testing.T) {
	old := plat
	defer func() { plat = old; resetFlags() }()
	const name = "\U0001F1FA\U0001F1F8 NODE-US-01"
	for _, tc := range []struct {
		goos, want string
	}{
		{"darwin", name},
		{"windows", "NODE-US-01"},
	} {
		plat = platformFor(tc.goos)
		resetFlags()
		if got := nodeText(name); got != tc.want {
			t.Errorf("%s: nodeText = %q, want %q", tc.goos, got, tc.want)
		}
	}
	// Linux probes the font: a pair shaped as one glyph keeps the flag.
	if !flagsShaped(20, 18, 18) || flagsShaped(30, 15, 15) || flagsShaped(0, 0, 0) {
		t.Error("flagsShaped")
	}
	plat = platformFor("linux")
	resetFlags()
	ui.NewTester(func(c *ui.Context) { probeFlags(c) }, 100, 100)
	if !flagsProbed {
		t.Error("linux: flags not probed on the first frame")
	}
}

// The Windows UI path shows no regional-indicator letters on any node
// surface: the panel's node row and the group tiles.
func TestNodeSurfacesNoFlagLetters(t *testing.T) {
	old := plat
	defer func() { plat = old; resetFlags() }()
	plat = platformFor("windows")
	resetFlags()
	a, s := layoutApp(t)
	tr := ui.NewTester(func(c *ui.Context) { a.mainView(c) }, 1280, 900)
	for _, p := range []string{pageOverview, pageProxies} {
		a.page = p
		tr.Frame()
		tr.Frame()
		for _, txt := range tr.Texts() {
			for _, r := range txt {
				if r >= 0x1F1E6 && r <= 0x1F1FF {
					t.Errorf("%s: %q shows a regional indicator", p, txt)
				}
			}
		}
		if !tr.HasText("NODE-US-01") {
			t.Errorf("%s: node name missing in %v", p, tr.Texts())
		}
	}
	_ = s
}
