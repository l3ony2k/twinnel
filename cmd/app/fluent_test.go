package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// usePlat sets plat to goos's for the test.
func usePlat(t *testing.T, goos string) {
	t.Helper()
	old := plat
	plat = platformFor(goos)
	t.Cleanup(func() { plat = old })
}

// luminance is the relative luminance of c (WCAG), opaque.
func luminance(c ui.Color) float64 {
	ch := func(v uint8) float64 {
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

func contrast(a, b ui.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// 2.6: on Windows the accent's fills are WinUI's shades of the system's
// accent, with text on them that reads (black on Light2 in the dark,
// white on Dark1 in the light), and links in the accent clear 4.5:1 on
// the page. macOS keeps the raw accent.
func TestFluentAccentShades(t *testing.T) {
	usePlat(t, "windows")
	raw := ui.Hex("#3073cf")
	p := ui.Preferences{Accent: raw}
	for _, dark := range []bool{true, false} {
		th := systemTheme(dark, p)
		if th.Accent == raw {
			t.Errorf("dark=%v: the raw accent was kept", dark)
		}
		if c := contrast(th.Accent, th.AccentText); c < 4.5 {
			t.Errorf("dark=%v: text on the accent %.2f:1", dark, c)
		}
		if c := contrast(th.Accent, th.Background); c < 4.5 {
			t.Errorf("dark=%v: accent (links) on the page %.2f:1", dark, c)
		}
	}
	if th := systemTheme(true, p); th.AccentText != ui.RGB(0, 0, 0) {
		t.Errorf("dark AccentText %v, want black", th.AccentText)
	}
	// Windows' own default blue lands on WinUI's shades.
	if th := systemTheme(true, ui.Preferences{}); th.Accent != ui.Hex("#60cdff") {
		t.Errorf("default dark accent %v", th.Accent)
	}
	if th := systemTheme(false, ui.Preferences{}); th.Accent != ui.Hex("#005fb8") {
		t.Errorf("default light accent %v", th.Accent)
	}
	usePlat(t, "darwin")
	if th := systemTheme(true, p); th.Accent != raw || th.AccentText != ui.RGB(255, 255, 255) {
		t.Errorf("macOS accent changed: %v on %v", th.AccentText, th.Accent)
	}
}

// withLightness keeps the hue: a blue stays blue.
func TestWithLightness(t *testing.T) {
	c := withLightness(ui.Hex("#0078d4"), 0.36)
	if c.B <= c.G || c.G <= c.R {
		t.Errorf("hue lost: %v", c)
	}
	if l := lightness(c); l < 0.35 || l > 0.37 {
		t.Errorf("lightness %.3f", l)
	}
	if g := withLightness(ui.Hex("#808080"), 0.2); g.R != g.G || g.G != g.B {
		t.Errorf("gray tinted: %v", g)
	}
}

// 2.11: Windows' focus visual is a 2 DIP opaque stroke in the text color,
// outside the control; macOS keeps its accent ring.
func TestFluentFocusVisual(t *testing.T) {
	usePlat(t, "windows")
	for _, dark := range []bool{true, false} {
		th := systemTheme(dark, ui.Preferences{})
		if th.FocusRingWidth != 2 || th.FocusRingOffset < 1 || th.Focus.A < 220 {
			t.Errorf("dark=%v: focus %v w%v off%v", dark, th.Focus, th.FocusRingWidth, th.FocusRingOffset)
		}
		if c := contrast(th.Focus.Over(th.Background), th.Background); c < 10 {
			t.Errorf("dark=%v: focus contrast %.1f", dark, c)
		}
	}
	usePlat(t, "darwin")
	if th := systemTheme(true, ui.Preferences{}); th.Focus.A == 255 {
		t.Error("macOS focus ring changed")
	}
}

// 2.10: every text size on Windows is on Fluent's ramp (Caption 12, Body
// 14, Body Strong/Subtitle 16/20, Title 28).
func TestFluentTypeRamp(t *testing.T) {
	p := platformFor("windows")
	ramp := map[float32]bool{12: true, 14: true, 16: true, 20: true, 28: true}
	for name, v := range map[string]float32{
		"body": p.bodySize, "small": p.smallSize, "callout": p.calloutSize, "lead": p.leadSize,
		"figure": p.figureSize, "title": p.titleSize, "heading": p.headingSize,
	} {
		if !ramp[v] {
			t.Errorf("%s %v is off the Fluent ramp", name, v)
		}
	}
	if platformFor("darwin").calloutSize != 12 {
		t.Error("macOS callout changed")
	}
}

// renderControls draws view on the Windows path in the app's theme and
// returns the tester.
func renderControls(t *testing.T, dark bool, view func(c *ui.Context)) *ui.Tester {
	t.Helper()
	tr := ui.NewTesterWith(func(c *ui.Context) {
		c.SetTheme(systemTheme(dark, c.Preferences()))
		th := c.Theme()
		ui.Column(c).Fill().Padding(20).Gap(12).Background(th.Background).Children(func() {
			groupBox(c, th).Children(func() { view(c) })
		})
	}, 480, 320, ui.TesterOptions{Dark: dark})
	tr.Frame()
	tr.Frame()
	return tr
}

func pixel(img *image.RGBA, x, y float32) ui.Color {
	c := img.RGBAAt(int(x), int(y))
	return ui.Color{R: c.R, G: c.G, B: c.B, A: 255}
}

func colorNear(a, b ui.Color, d int) bool {
	abs := func(v int) int {
		if v < 0 {
			return -v
		}
		return v
	}
	return abs(int(a.R)-int(b.R)) <= d && abs(int(a.G)-int(b.G)) <= d && abs(int(a.B)-int(b.B)) <= d
}

// 2.3/2.4: a disabled primary, however it was built (primaryButton,
// opAction), wears the disabled face, not a washed-out accent; on
// Windows the face is WinUI's disabled button, quieter than an enabled
// push button.
func TestDisabledPrimaryFaces(t *testing.T) {
	usePlat(t, "windows")
	a, _ := layoutApp(t)
	for _, dark := range []bool{true, false} {
		tr := renderControls(t, dark, func(c *ui.Context) {
			ui.Row(c).Gap(12).Children(func() {
				primaryButton(c.Key("on"), "Enabled", false)
				primaryButton(c.Key("off"), "Disabled", true)
				a.opAction(c.Key("op"), iconOK, "OpOff", true, true, "x")
				pushButton(c.Key("push"), "Push")
			})
		})
		img := tr.Image()
		th := systemTheme(dark, ui.Preferences{})
		at := func(label string) ui.Color {
			r, ok := tr.Find(label)
			if !ok {
				t.Fatalf("no %q in %q", label, tr.Texts())
			}
			// Left of the label, inside the button's padding.
			return pixel(img, r.X-6, r.Y+r.H/2)
		}
		if on := at("Enabled"); !colorNear(on, th.Accent, 24) {
			t.Errorf("dark=%v: enabled primary %v, want the accent %v", dark, on, th.Accent)
		}
		for _, l := range []string{"Disabled", "OpOff"} {
			off := at(l)
			if colorNear(off, th.Accent, 60) || colorNear(off, th.Accent.Mix(th.Background, 0.5), 30) {
				t.Errorf("dark=%v: %s keeps the accent: %v", dark, l, off)
			}
			if push := at("Push"); contrast(off, pal(th).group.Over(th.Background)) > contrast(push, pal(th).group.Over(th.Background))+0.05 {
				t.Errorf("dark=%v: disabled %s (%v) louder than an enabled push button (%v)", dark, l, off, push)
			}
		}
	}
}

// fileCalls counts, per file of the app (tests left out), the calls fn
// reports as matching.
func fileCalls(t *testing.T, match func(fn *ast.FuncDecl, call *ast.CallExpr) bool) map[string]int {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && match(fn, call) {
					out[name]++
				}
				return true
			})
		}
	}
	return out
}

// isCall reports whether e calls pkg.name (pkg "" for a plain function or
// any method of that name).
func isCall(e ast.Expr, pkg, name string) (*ast.CallExpr, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	switch f := call.Fun.(type) {
	case *ast.SelectorExpr:
		if f.Sel.Name != name {
			return nil, false
		}
		if id, ok := f.X.(*ast.Ident); pkg != "" && (!ok || id.Name != pkg) {
			return nil, false
		}
		return call, true
	case *ast.Ident:
		return call, pkg == "" && f.Name == name
	}
	return nil, false
}

// checkMigrations fails on a file with more matches than known, and logs
// those still to migrate (owned by other areas; see HANDOFF).
func checkMigrations(t *testing.T, what string, got, known map[string]int) {
	t.Helper()
	for f, n := range got {
		if n > known[f] {
			t.Errorf("%s: %d %s (known %d); %s", f, n, what, known[f], "use the platform helper")
		} else if n > 0 {
			t.Logf("%s: %d %s still to migrate", f, n, what)
		}
	}
}

// 2.3: a primary button is disabled while it is built, so its face can
// show it: no ui.PrimaryButton(…).Disabled(…), opButton(…, primary,
// …).Disabled(…) with primary not false, or iconButton(…, true)…
// outside disabledPrimaryFace. Sites in other areas' files are counted
// until they move to primaryButton/opAction (HANDOFF).
func TestDisabledPrimariesKnowIt(t *testing.T) {
	got := fileCalls(t, func(fn *ast.FuncDecl, call *ast.CallExpr) bool {
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Disabled" {
			return false
		}
		if _, ok := isCall(sel.X, "ui", "PrimaryButton"); ok {
			return true
		}
		for _, name := range []string{"opButton", "iconButton", "busyButton"} {
			inner, ok := isCall(sel.X, "", name)
			if !ok {
				continue
			}
			arg := 3 // opButton(c, icon, label, primary, op), iconButton(c, icon, label, primary)
			if name == "busyButton" {
				arg = 2
			}
			if len(inner.Args) > arg {
				if id, ok := inner.Args[arg].(*ast.Ident); ok && id.Name == "false" {
					return false
				}
			}
			return true
		}
		return false
	})
	checkMigrations(t, "primaries disabled after they were built", got, map[string]int{
		"shots_surfaces.go": 1, // MyGo's own disabled primary, shown for comparison
	})
}

// 2.2/2.5: switches and spinners are the platform's (toggleSwitch,
// spinner): a bare ui.Switch or ui.Spinner draws the macOS one on
// Windows. Sites in other areas' files are counted until they move.
func TestSpinnersArePlatform(t *testing.T) {
	spin := fileCalls(t, func(fn *ast.FuncDecl, call *ast.CallExpr) bool {
		_, ok := isCall(call, "ui", "Spinner")
		return ok && fn.Name.Name != "spinner" && fn.Name.Name != "spinnerIcon"
	})
	checkMigrations(t, "ui.Spinner", spin, map[string]int{})
	sw := fileCalls(t, func(fn *ast.FuncDecl, call *ast.CallExpr) bool {
		_, ok := isCall(call, "ui", "Switch")
		return ok && fn.Name.Name != "toggleSwitch"
	})
	checkMigrations(t, "ui.Switch", sw, map[string]int{})
}

// 2.2: WinUI's ToggleSwitch: off, a stroked track with a knob in the
// secondary text color (MyGo's white knob on a light gray track vanished
// on a light card); on, the accent with the knob in the text on it.
func TestFluentSwitchFace(t *testing.T) {
	usePlat(t, "windows")
	for _, dark := range []bool{false, true} {
		off, on := false, true
		tr := renderControls(t, dark, func(c *ui.Context) {
			ui.Row(c).Gap(40).Children(func() {
				toggleSwitch(c.Key("off"), &off).Label("off")
				toggleSwitch(c.Key("on"), &on).Label("on")
			})
		})
		img := tr.Image()
		th := systemTheme(dark, ui.Preferences{})
		card := pal(th).group.Over(th.Background)
		ro, _ := tr.Find("off")
		rn, _ := tr.Find("on")
		if ro.W != 40 || ro.H != 20 {
			t.Fatalf("switch %v×%v, want 40×20", ro.W, ro.H)
		}
		// The knob of the off switch: at its leading end.
		k := pixel(img, ro.X+10, ro.Y+10)
		if contrast(k, card) < 3 {
			t.Errorf("dark=%v: off knob %v on card %v", dark, k, card)
		}
		// The track's edge is stroked, its inside clear.
		edge, inside := pixel(img, ro.X+20, ro.Y+0.5), pixel(img, ro.X+20, ro.Y+10)
		if contrast(edge, card) < 2 || contrast(inside, card) > 1.3 {
			t.Errorf("dark=%v: off track edge %v inside %v card %v", dark, edge, inside, card)
		}
		if f := pixel(img, rn.X+12, rn.Y+10); !colorNear(f, th.Accent, 24) {
			t.Errorf("dark=%v: on track %v, want the accent", dark, f)
		}
		if kn := pixel(img, rn.X+30, rn.Y+10); !colorNear(kn, th.AccentText, 40) {
			t.Errorf("dark=%v: on knob %v, want %v", dark, kn, th.AccentText)
		}
	}
}

// 2.1: on Windows a page's segmented choice is a row of radio buttons
// (one group, Changed on it); log source and client tabs use selectorBar,
// whose chosen tab carries the accent pill.
func TestFluentChoiceControls(t *testing.T) {
	usePlat(t, "windows")
	sel, tab := 0, 0
	changed := false
	tr := renderControls(t, true, func(c *ui.Context) {
		if segmented(c.Key("mode"), &sel, "规则", "全局", "直连").Label("mode").Changed() {
			changed = true
		}
		selectorBar(c.Key("src"), &tab, "mihomo", "Twinnel", "助手").Label("src")
	})
	if err := tr.Click("全局"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if sel != 1 || !changed {
		t.Errorf("radio row: sel %d changed %v", sel, changed)
	}
	if err := tr.Click("助手"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	tr.Frame()
	if tab != 2 {
		t.Errorf("selector bar: tab %d", tab)
	}
	img := tr.Image()
	th := systemTheme(true, ui.Preferences{})
	r, _ := tr.Find("助手")
	found := false
	for y := r.Y + r.H; y < r.Y+r.H+14 && !found; y++ {
		found = colorNear(pixel(img, r.X+r.W/2, y), th.Accent, 30)
	}
	if !found {
		t.Error("no accent pill under the chosen tab")
	}
	// The chosen radio's ring is the accent.
	rr, _ := tr.Find("全局")
	if !colorNear(pixel(img, rr.X-18, rr.Y+rr.H/2-8), th.Accent, 30) {
		t.Errorf("chosen radio ring %v", pixel(img, rr.X-18, rr.Y+rr.H/2-8))
	}
}

// 2.8: the disclosure chevron is WinUI's on Windows (down, up when open,
// after the row's controls) and NSOutlineView's elsewhere.
func TestDisclosureFollowsPlatform(t *testing.T) {
	usePlat(t, "windows")
	if chevronTurn(false) != 90 || chevronTurn(true) != 270 {
		t.Errorf("windows turns %v/%v", chevronTurn(false), chevronTurn(true))
	}
	usePlat(t, "darwin")
	if chevronTurn(false) != 0 || chevronTurn(true) != 90 {
		t.Errorf("macOS turns %v/%v", chevronTurn(false), chevronTurn(true))
	}
}

// 2.8: an inline disclosure on Windows is in the text color (an Expander
// header); on macOS an accent link.
func TestLinkDisclosureFace(t *testing.T) {
	for _, goos := range []string{"windows", "darwin"} {
		usePlat(t, goos)
		tr := renderControls(t, true, func(c *ui.Context) {
			linkDisclosure(c.Key("d"), false, "Advanced")
		})
		img := tr.Image()
		th := systemTheme(true, ui.Preferences{})
		r, _ := tr.Find("Advanced")
		_ = th
		accentLeft := false // a blue pixel in the disclosure (the accent is blue here)
		for x := r.X; x < r.X+r.W; x++ {
			for y := r.Y; y < r.Y+r.H; y++ {
				if p := pixel(img, x, y); int(p.B)-int(p.R) > 60 {
					accentLeft = true
				}
			}
		}
		if goos == "windows" && accentLeft {
			t.Error("windows: an accent link")
		}
		if goos == "darwin" && !accentLeft {
			t.Error("macOS: not an accent link")
		}
	}
}

// 2.7: Windows shows one toast at a time, the newest; macOS stacks.
func TestToastsShown(t *testing.T) {
	list := []ui.Toast{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	show, drop := toastsShown(list, true)
	if len(show) != 1 || show[0].ID != "c" || strings.Join(drop, ",") != "a,b" {
		t.Errorf("one: show %v drop %v", show, drop)
	}
	if show, drop := toastsShown(list, false); len(show) != 3 || drop != nil {
		t.Errorf("stack: show %v drop %v", show, drop)
	}
}

// 6.9: a chosen list row on Windows is a subtle fill with the text in its
// color, not the accent with AccentText; elsewhere the theme stays.
func TestListSelectionTheme(t *testing.T) {
	usePlat(t, "windows")
	th := systemTheme(true, ui.Preferences{})
	u := listSelectionTheme(th)
	if u == nil || u.AccentText != th.Text || contrast(u.Accent, th.Background) > 1.4 {
		t.Fatalf("windows selection %+v", u)
	}
	if th.Accent == u.Accent {
		t.Error("the window's theme changed")
	}
	usePlat(t, "darwin")
	if listSelectionTheme(th) != nil {
		t.Error("macOS selection changed")
	}
}

// 2.12: UI strings end with the ellipsis character, never three dots.
func TestNoASCIIEllipsis(t *testing.T) {
	for _, l := range i18n.Langs() {
		walkStrings(reflect.ValueOf(i18n.For(l.Tag)), "", func(path, s string) {
			if strings.Contains(s, "...") {
				t.Errorf("%s %s: %q has three dots; use …", l.Tag, path, s)
			}
		})
	}
}

func walkStrings(v reflect.Value, path string, fn func(path, s string)) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			walkStrings(v.Elem(), path, fn)
		}
	case reflect.String:
		fn(path, v.String())
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				walkStrings(v.Field(i), path+"."+v.Type().Field(i).Name, fn)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			walkStrings(v.Index(i), path, fn)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			walkStrings(v.MapIndex(k), path, fn)
		}
	}
}
