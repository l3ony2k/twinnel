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

// TestWindowsControlShots renders the shared controls on the Windows UI
// path, dark and light, into TWINNEL_WINSHOTS/win-controls-<theme>.png:
// radio row, selector bar, switches, buttons (enabled, disabled, busy),
// disclosures, a progress ring, a toast. Opt-in as TestWindowsShots.
func TestWindowsControlShots(t *testing.T) {
	dir := os.Getenv("TWINNEL_WINSHOTS")
	if dir == "" {
		t.Skip("TWINNEL_WINSHOTS not set")
	}
	scale, err := strconv.ParseFloat(cmpOr(os.Getenv("TWINNEL_WINSHOTS_SCALE"), "1.5"), 32)
	if err != nil {
		t.Fatal(err)
	}
	usePlat(t, "windows")
	a, _ := layoutApp(t)
	for _, dark := range []bool{true, false} {
		mode, src, on, off, open := 0, 1, true, false, true
		tr := ui.NewTesterWith(func(c *ui.Context) {
			c.SetTheme(systemTheme(dark, c.Preferences()))
			th := c.Theme()
			ui.Column(c).Fill().Padding(24).Gap(16).Background(th.Background).Children(func() {
				card(c, "Controls", func() {
					kvWith(c, "模式", func() { kvControl(segmented(c.Key("mode"), &mode, "规则", "全局", "直连")) })
					kvWith(c, "来源", func() { selectorBar(c.Key("src"), &src, "mihomo", "Twinnel", "后台助手") })
					kvWith(c, "开关", func() {
						toggleSwitch(c.Key("on"), &on).Label("on")
						toggleSwitch(c.Key("off"), &off).Label("off")
						toggleSwitch(c.Key("dis"), &off).Label("dis").Disabled(true)
					})
					kvWith(c, "按钮", func() {
						primaryButton(c.Key("p"), "下一步", false)
						primaryButton(c.Key("pd"), "下一步", true)
						a.opAction(c.Key("od"), iconOK, "应用", true, true, "x")
						pushButton(c.Key("b"), "上一步")
						pushButton(c.Key("bd"), "上一步").Disabled(true)
					})
					kvWith(c, "进行中", func() {
						spinner(c)
						ui.Button(c.Key("busy"), "").Children(func() {
							spinnerIcon(c)
							ui.Text(c, "检查更新").SingleLine()
						})
					})
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						disclosureStart(c, open)
						ui.Text(c, "订阅 · example").Grow(1)
						toggleSwitch(c.Key("row"), &on).Label("row")
						disclosureEnd(c, open)
					})
					linkDisclosure(c.Key("adv"), false, "高级")
					ui.Button(c.Key("focus"), "检测出口 IP").AutoFocus()
				})
				toastView(c, ui.Toast{ID: "t", Title: "已应用", Type: toastSuccess})
			})
		}, 760, 560, ui.TesterOptions{Dark: dark})
		tr.SetScale(float32(scale))
		tr.Key(0, ui.KeyTab)
		tr.Frame()
		tr.Frame()
		name := "win-controls-light.png"
		if dark {
			name = "win-controls-dark.png"
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tr.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
