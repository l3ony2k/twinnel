package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// TestWindowsShotsPanel renders the tray flyout on the Windows UI path
// from the synthetic snapshot, headless, in dark and light, zh and en, as
// TestWindowsShots renders the pages (same opt-in variables):
//
//	TWINNEL_WINSHOTS=<scratch dir> TWINNEL_WINSHOTS_SCALE=1.5 \
//	go test ./cmd/app -run TestWindowsShotsPanel
func TestWindowsShotsPanel(t *testing.T) {
	dir := os.Getenv("TWINNEL_WINSHOTS")
	if dir == "" {
		t.Skip("TWINNEL_WINSHOTS not set")
	}
	scale, err := strconv.ParseFloat(cmpOr(os.Getenv("TWINNEL_WINSHOTS_SCALE"), "1.5"), 32)
	if err != nil {
		t.Fatal(err)
	}
	old, oldT := plat, i18n.T
	defer func() { plat, i18n.T = old, oldT; resetFlags() }()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	shot := func(goos, lang, theme, variant string) {
		plat = platformFor(goos)
		resetFlags()
		i18n.Set(lang)
		a, _ := layoutApp(t)
		a.theme = theme
		if variant == "helper" { // the helper installed: 应急恢复 in the foot
			snap := a.shotSnap
			a.shotSnap = func(s *status.Snapshot) { snap(s); s.Helper.State = status.HelperConnected }
		}
		tr := ui.NewTesterWith(func(c *ui.Context) { a.panelView(c) }, panelWidth, 600, ui.TesterOptions{Dark: theme == "dark"})
		tr.SetScale(float32(scale))
		tr.Frame()
		tr.Frame()
		name := filepath.Join(dir, map[string]string{"windows": "win", "darwin": "mac"}[goos]+"-panel-"+lang+"-"+theme+variant+".png")
		f, err := os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tr.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
		t.Logf("saved %s", name)
	}
	for _, lang := range []string{"zh", "en"} {
		for _, theme := range []string{"dark", "light"} {
			shot("windows", lang, theme, "")
		}
	}
	shot("windows", "zh", "dark", "helper")
	shot("darwin", "zh", "dark", "helper") // macOS stays as it was
}
