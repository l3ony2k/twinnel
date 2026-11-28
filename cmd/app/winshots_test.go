package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestWindowsShots renders pages on the Windows UI path (plat for
// windows: Fluent theme, Segoe metrics, the hidden title bar's room) from
// the synthetic snapshot, headless, into PNGs: a hermetic look at the
// Windows layout from any machine. Opt-in:
//
//	TWINNEL_WINSHOTS=<scratch dir> \
//	TWINNEL_WINSHOTS_PAGES=overview,health,takeover,logs,leave \
//	TWINNEL_WINSHOTS_SCALE=1.5 TWINNEL_WINSHOTS_WIDTH=1717 \
//	go test ./cmd/app -run TestWindowsShots
//
// Each page is opened as TWINNEL_OPEN=page:<id> opens it (parseLaunchOpen).
// The width is in DIPs: 1717 at 1.5 is a 2575 px window. MyGo draws the
// caption buttons natively, so the shots show their room, not them.
func TestWindowsShots(t *testing.T) {
	dir := os.Getenv("TWINNEL_WINSHOTS")
	if dir == "" {
		t.Skip("TWINNEL_WINSHOTS not set")
	}
	scale, err := strconv.ParseFloat(cmpOr(os.Getenv("TWINNEL_WINSHOTS_SCALE"), "1.5"), 32)
	if err != nil {
		t.Fatal(err)
	}
	width, err := strconv.Atoi(cmpOr(os.Getenv("TWINNEL_WINSHOTS_WIDTH"), "1717"))
	if err != nil {
		t.Fatal(err)
	}
	pages := strings.Split(cmpOr(os.Getenv("TWINNEL_WINSHOTS_PAGES"), "overview,health,takeover,logs,leave,proxies"), ",")
	old := plat
	defer func() { plat = old; resetFlags() }()
	plat = platformFor("windows")
	resetFlags()
	a, _ := layoutApp(t)
	a.probes = &shotProbes{goos: "windows", machine: shotMachine("windows")} // Windows fixtures (ui-review 8.6)
	a.theme = "dark"
	tr := ui.NewTesterWith(func(c *ui.Context) { a.mainView(c) }, width, width*945/1717, ui.TesterOptions{Dark: true})
	tr.SetTitleBar(titleBarFor("windows"))
	tr.SetScale(float32(scale))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var digests shotDigests
	for _, p := range pages {
		o, err := parseLaunchOpen("page:" + p)
		if err != nil {
			t.Fatal(err)
		}
		prev := a.page
		a.page = o.page
		a.pageChanged(prev)
		tr.Frame()
		tr.Frame()
		var buf bytes.Buffer
		if err := png.Encode(&buf, tr.Image()); err != nil {
			t.Fatal(err)
		}
		name := filepath.Join(dir, "win-"+p+".png")
		if err := os.WriteFile(name, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("saved %s", name)
		digests.add("win-"+p, buf.Bytes())
	}
	if err := digests.err(); err != nil {
		t.Error(err) // two pages drew the same: the page switch did not render
	}
}

func cmpOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
