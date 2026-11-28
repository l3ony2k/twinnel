package main

import (
	"go/ast"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
)

func pageStateNamed(t *testing.T, name string) pageState {
	t.Helper()
	for _, st := range pageStates() {
		if st.name == name {
			return st
		}
	}
	t.Fatalf("no page state %q", name)
	return pageState{}
}

func zhOn(t *testing.T, goos string) {
	t.Helper()
	usePlat(t, goos)
	old := i18n.T
	i18n.Set("zh")
	t.Cleanup(func() { i18n.T = old; resetFlags() })
	resetFlags()
}

func hasText(tr *ui.Tester, s string) bool {
	for _, x := range tr.Texts() {
		if strings.Contains(x, s) {
			return true
		}
	}
	return false
}

// 6.5: a download whose size is known shows how far it is as a
// determinate bar; only a stage without a measure spins.
func TestProgressFraction(t *testing.T) {
	for _, c := range []struct {
		p     *rpc.AccessoryProgress
		frac  float64
		known bool
	}{
		{nil, 0, false},
		{&rpc.AccessoryProgress{Stage: rpc.AccStageDownload, Done: 4, Total: 10}, 0.4, true},
		{&rpc.AccessoryProgress{Stage: rpc.AccStageDownload, Done: 4}, 0, false},
		{&rpc.AccessoryProgress{Stage: rpc.AccStageVerify, Done: 4, Total: 10}, 0, false},
		{&rpc.AccessoryProgress{Stage: rpc.AccStageDownload, Done: 12, Total: 10}, 1, true},
	} {
		if f, k := progressFraction(c.p); f != c.frac || k != c.known {
			t.Errorf("%+v: %v %v", c.p, f, k)
		}
	}
}

// 6.6/6.7/6.8: a component that is not there is one row (name, state
// under it, the download), with no explanatory sentence; a failed
// download offers 重试 and keeps the other routes under one disclosure;
// the 未安装 state goes while it downloads.
func TestComponentRows(t *testing.T) {
	for _, goos := range []string{"windows", "darwin"} {
		zhOn(t, goos)
		T := i18n.T
		tr := renderPageState(t, goos, pageStateNamed(t, "components-not-installed"), 1)
		for _, s := range []string{"没有它，这些规则不生效", "Twinnel 本身不需要它"} {
			if hasText(tr, s) {
				t.Errorf("%s: the sentence %q is back (the ⓘ says it)", goos, s)
			}
		}
		geo, _ := tr.Find(T.GeoTitle)
		dl, ok := tr.Find("下载 · 25.7 MB")
		if !ok {
			t.Fatalf("%s: no download in %q", goos, tr.Texts())
		}
		if dl.Y > geo.Y+geo.H+20 {
			t.Errorf("%s: the download sits below the name (%v vs %v), not on its row", goos, dl, geo)
		}

		tr = renderPageState(t, goos, pageStateNamed(t, "components-failed"), 1)
		if !hasText(tr, "没能下载 GEO 数据库") || !hasText(tr, T.Retry) {
			t.Errorf("%s: failed row %q", goos, tr.Texts())
		}
		if hasText(tr, T.CompViaSub) {
			t.Errorf("%s: the other routes show before they are asked for", goos)
		}
		if err := tr.Click(T.CompOtherRoutes); err != nil {
			t.Fatal(err)
		}
		tr.Frame()
		tr.Frame()
		if !hasText(tr, T.CompViaSub) {
			t.Errorf("%s: 其他下载方式 did not open", goos)
		}

		st := pageStateNamed(t, "components-downloading")
		set := st.set
		st.set = func(a *app) { // the dashboard, not there, downloading
			set(a)
			a.hd.busy = uiKind().opUpdate()
			a.beginOp(a.hd.busy)
		}
		tr = renderPageState(t, goos, st, 1)
		n := 0
		for _, x := range tr.Texts() {
			if x == T.AccessoryMissing {
				n++
			}
		}
		if n != 0 {
			t.Errorf("%s: 未安装 shows while it downloads", goos)
		}
	}
}

// 6.9: on Windows the chosen history row is a subtle fill with the text
// in its color and the accent pill at its start, not the accent bar.
func TestHistorySelectionFluent(t *testing.T) {
	zhOn(t, "windows")
	tr := renderPageState(t, "windows", pageStateNamed(t, "history"), 1)
	img := tr.Image()
	th := systemTheme(true, ui.Preferences{})
	r, ok := tr.Find("11-22 02:00:00")
	if !ok {
		t.Fatalf("no chosen row in %q", tr.Texts())
	}
	bg := pixel(img, r.X+r.W+20, r.Y+r.H/2)
	if colorNear(bg, th.Accent, 60) {
		t.Errorf("chosen row filled with the accent: %v", bg)
	}
	pill := false
	for x := r.X - 14; x < r.X; x++ {
		if colorNear(pixel(img, x, r.Y+r.H/2), th.Accent, 40) {
			pill = true
		}
	}
	if !pill {
		t.Error("no accent pill at the chosen row's start")
	}
}

// 6.10: a short diff's pane is as tall as the diff, not a fixed 380.
func TestDiffPaneFitsDiff(t *testing.T) {
	zhOn(t, "windows")
	st := pageStateNamed(t, "history")
	var a0 *app
	set := st.set
	st.set = func(a *app) { set(a); a0 = a }
	renderPageState(t, "windows", st, 1)
	h := diffHeight(a0.hd.hist.diff)
	if h >= diffMaxH || h < 3*codeLineH {
		t.Errorf("a 6-line diff gets a %v pane", h)
	}
}

// 2.1: a choice in a page's form is the platform's (pageChoice: radios on
// Windows); a bare ui.Segmented draws a segmented control there.
func TestPageChoicesArePlatform(t *testing.T) {
	got := fileCalls(t, func(fn *ast.FuncDecl, call *ast.CallExpr) bool {
		_, ok := isCall(call, "ui", "Segmented")
		return ok && fn.Name.Name != "pageChoice"
	})
	checkMigrations(t, "ui.Segmented", got, map[string]int{})
}
