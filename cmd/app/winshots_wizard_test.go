package main

import (
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestWindowsWizardShots renders the first-run wizard's states (the shot
// flow's list, wizShotList) on the Windows UI path, headless, into PNGs
// named wiz-<state>.png: TestWindowsShots for the wizard. Opt-in:
//
//	TWINNEL_WINSHOTS=<scratch dir> \
//	TWINNEL_WINSHOTS_WIZARD=helper,ts-failed \  # default: every state
//	TWINNEL_WINSHOTS_SCALE=1.5 TWINNEL_WINSHOTS_WIDTH=1100 \
//	TWINNEL_WINSHOTS_GOOS=windows \              # darwin, linux: their look
//	go test ./cmd/app -run TestWindowsWizardShots
func TestWindowsWizardShots(t *testing.T) {
	dir := os.Getenv("TWINNEL_WINSHOTS")
	if dir == "" {
		t.Skip("TWINNEL_WINSHOTS not set")
	}
	scale, err := strconv.ParseFloat(cmpOr(os.Getenv("TWINNEL_WINSHOTS_SCALE"), "1.5"), 32)
	if err != nil {
		t.Fatal(err)
	}
	width, err := strconv.Atoi(cmpOr(os.Getenv("TWINNEL_WINSHOTS_WIDTH"), "1100"))
	if err != nil {
		t.Fatal(err)
	}
	goos := cmpOr(os.Getenv("TWINNEL_WINSHOTS_GOOS"), "windows")
	var only []string
	if v := os.Getenv("TWINNEL_WINSHOTS_WIZARD"); v != "" {
		only = strings.Split(v, ",")
	}
	old := plat
	defer func() { plat = old; resetFlags() }()
	plat = platformFor(goos)
	resetFlags()
	a, _ := layoutApp(t)
	a.theme = "dark"
	tr := ui.NewTesterWith(func(c *ui.Context) { a.mainView(c) }, width, 820, ui.TesterOptions{Dark: true})
	tr.SetTitleBar(titleBarFor(goos))
	tr.SetScale(float32(scale))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sh := range wizShotList(goos) {
		if only != nil && !slices.Contains(only, sh.name) {
			continue
		}
		prev := a.page
		a.page = pageWizard
		if sh.page != "" {
			a.page = sh.page
		}
		a.pageChanged(prev)
		a.hd.onb = onboardingView{loaded: true}
		a.applyWizShot(sh, goos)
		tr.Frame()
		tr.Frame()
		tr.Frame()
		p := filepath.Join(dir, "wiz-"+sh.name+".png")
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tr.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
