package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// shellTester renders the main window on goos's shell (plat, title bar).
func shellTester(t *testing.T, goos string, w, h int) (*app, *ui.Tester) {
	t.Helper()
	usePlat(t, goos)
	resetFlags()
	t.Cleanup(resetFlags)
	a, _ := layoutApp(t)
	tr := ui.NewTester(func(c *ui.Context) { a.mainView(c) }, w, h)
	tr.SetTitleBar(titleBarFor(goos))
	tr.Frame()
	tr.Frame()
	return a, tr
}

// accessNamed is the first element assistive technology sees named name
// with a role other than the toolbar's (labeled with the page's title).
func accessNamed(tr *ui.Tester, name string) (ui.AccessInfo, bool) {
	for _, e := range tr.AccessAll() {
		if e.Name == name && e.Role != ui.RoleToolbar {
			return e, true
		}
	}
	return ui.AccessInfo{}, false
}

// ui-review 3.1: on Windows the sidebar's icons are in the text color and
// only the selection pill is the accent (accent icons read as links);
// macOS keeps Finder's accent icons.
func TestNavIconsFollowPlatform(t *testing.T) {
	th := systemTheme(true, ui.Preferences{})
	usePlat(t, "windows")
	if got := navIconColor(th); got != th.Text {
		t.Errorf("windows: icons %v, want the text color %v", got, th.Text)
	}
	usePlat(t, "darwin")
	if got := navIconColor(th); got != th.Accent {
		t.Errorf("macOS: icons %v, want the accent %v", got, th.Accent)
	}
	usePlat(t, "linux")
	if got := navIconColor(th); got != th.Accent {
		t.Errorf("linux: icons %v, want the accent (unchanged)", got)
	}
}

// ui-review 3.1, rendered: on Windows no sidebar row but the chosen one's
// pill carries the accent (the accent is a blue here).
func TestWindowsSidebarHasOnlyThePill(t *testing.T) {
	_, tr := shellTester(t, "windows", 1280, 800)
	img := tr.Image()
	blue := 0
	for _, label := range []string{i18n.T.PageProxies, i18n.T.PageHealth, i18n.T.PageSettings} {
		r, ok := tr.Find(label)
		if !ok {
			t.Fatalf("no %q", label)
		}
		// The icon: left of the label.
		for x := r.X - 26; x < r.X-4; x++ {
			for y := r.Y; y < r.Y+r.H; y++ {
				if p := pixel(img, x, y); int(p.B)-int(p.R) > 80 {
					blue++
				}
			}
		}
	}
	if blue > 0 {
		t.Errorf("%d accent pixels in unselected rows' icons", blue)
	}
}

// ui-review 7.1: a sidebar row reports itself selected, not checked. On
// Windows the list is NavigationView's (a list box of options: UIA List
// and ListItem with SelectionItem); macOS keeps Finder's outline.
func TestNavRowsReportSelected(t *testing.T) {
	for _, goos := range []string{"windows", "darwin", "linux"} {
		_, tr := shellTester(t, goos, 1280, 800)
		e, ok := accessNamed(tr, i18n.T.PageOverview)
		if !ok {
			t.Fatalf("%s: no row %q", goos, i18n.T.PageOverview)
		}
		if !e.Selected || e.Checked {
			t.Errorf("%s: overview row selected=%v checked=%v", goos, e.Selected, e.Checked)
		}
		if o, _ := accessNamed(tr, i18n.T.PageProxies); o.Selected {
			t.Errorf("%s: an unchosen row reports selected", goos)
		}
		wantList, wantItem := ui.RoleTree, ui.RoleTreeItem
		if goos == "windows" {
			wantList, wantItem = ui.RoleListBox, ui.RoleOption
		}
		if e.Role != wantItem {
			t.Errorf("%s: row role %v, want %v", goos, e.Role, wantItem)
		}
		if l, ok := accessNamed(tr, "Twinnel"); !ok || l.Role != wantList {
			t.Errorf("%s: list role %v (found %v), want %v", goos, l.Role, ok, wantList)
		}
	}
}

// focusedAccess is the element with the keyboard focus, as assistive
// technology sees it.
func focusedAccess(tr *ui.Tester) ui.AccessInfo {
	for _, e := range tr.AccessAll() {
		if e.Focused {
			return e
		}
	}
	return ui.AccessInfo{}
}

// ui-review 7.4: the sidebar toggle, the window's top-left control, is
// the first stop of Tab; F6 moves between the sidebar and the page on
// Windows (and Linux), not on macOS.
func TestToggleFirstAndF6(t *testing.T) {
	for _, goos := range []string{"windows", "linux", "darwin"} {
		_, tr := shellTester(t, goos, 1280, 800)
		step := func(mods ui.Modifiers, key ui.Key) ui.AccessInfo {
			tr.Key(mods, key)
			tr.Frame()
			tr.Frame()
			return focusedAccess(tr)
		}
		if f := step(0, ui.KeyTab); f.Name != i18n.T.SidebarToggle {
			t.Errorf("%s: Tab's first stop is %q, not the sidebar toggle", goos, f.Name)
		}
		if goos == "darwin" {
			if f := step(0, ui.KeyF6); f.Name != i18n.T.SidebarToggle {
				t.Errorf("macOS: F6 moved the focus to %q", f.Name)
			}
			continue
		}
		if f := step(0, ui.KeyF6); f.Name != "Twinnel" {
			t.Errorf("%s: F6 from the toggle reached %q, not the sidebar", goos, f.Name)
		}
		if f := step(0, ui.KeyF6); f.Name != i18n.T.PageOverview || f.Bounds.X < 200 {
			t.Errorf("%s: F6 from the sidebar reached %q at %+v, not the page", goos, f.Name, f.Bounds)
		}
		if f := step(ui.Shift, ui.KeyF6); f.Name != "Twinnel" {
			t.Errorf("%s: Shift+F6 from the page reached %q, not the sidebar", goos, f.Name)
		}
	}
}

// ui-review 3.4: Windows' icon rail is NavigationView's compact pane, 48
// DIP with 40 DIP items, and the toggle stays at the top of the pane in
// the same place in every mode (it moved beside the page's title).
func TestWindowsRailAndToggle(t *testing.T) {
	a, tr := shellTester(t, "windows", 1280, 800)
	where := func() ui.Rect {
		e, ok := accessNamed(tr, i18n.T.SidebarToggle)
		if !ok {
			t.Fatal("no toggle")
		}
		return e.Bounds
	}
	expanded := where()
	for _, m := range []sidebarMode{sideRail, sideHidden} {
		a.side.setMode(m)
		tr.Frame()
		tr.Frame()
		if r := where(); r != expanded {
			t.Errorf("mode %v: toggle at %+v, expanded at %+v", m, r, expanded)
		}
		if m == sideRail {
			if w := a.side.widthOf(sideRail); w != 48 {
				t.Errorf("rail %v DIP, want 48", w)
			}
			e, ok := accessNamed(tr, i18n.T.PageProxies)
			if !ok || e.Bounds.W != 40 || e.Bounds.X < 4-0.5 || e.Bounds.X+e.Bounds.W > 44+0.5 {
				t.Errorf("rail item %+v (found %v), want 40 wide inside 4…44", e.Bounds, ok)
			}
		}
	}
	if expanded.X+expanded.W > 48 {
		t.Errorf("toggle %+v not over the compact pane", expanded)
	}
	// macOS keeps its toggle after the lights, beside the rail.
	usePlat(t, "darwin")
	if railRowWidth() != railRowW || sideToggleWidth() != sideToggleW {
		t.Error("macOS rail changed")
	}
}

// ui-review 3.3: Windows keeps the page's column at the pane's leading
// edge and leaves the slack on the right; macOS and Linux center it.
func TestColumnInsetsPerPlatform(t *testing.T) {
	usePlat(t, "windows")
	pad := plat.pagePadX
	if l, r := columnInsets(3000, 1000); l != pad || r != 3000-pad-1000 {
		t.Errorf("windows: %v/%v", l, r)
	}
	if l, r := columnInsets(500, 1000); l != pad || r != pad {
		t.Errorf("windows narrow: %v/%v", l, r)
	}
	for _, goos := range []string{"darwin", "linux"} {
		usePlat(t, goos)
		if l, r := columnInsets(3000, 1000); l != r || l != columnInset(3000, 1000) {
			t.Errorf("%s: %v/%v not centered", goos, l, r)
		}
	}
}

// ui-review 3.5: a sub-page's back button on Windows is a borderless
// glyph (its box shows the page's background at rest); macOS keeps its
// push button.
func TestBackButtonFollowsPlatform(t *testing.T) {
	for _, goos := range []string{"windows", "darwin"} {
		a, tr := shellTester(t, goos, 1280, 800)
		a.page = pageInstall
		tr.Frame()
		tr.Frame()
		e, ok := accessNamed(tr, i18n.T.NavBack)
		if !ok {
			t.Fatalf("%s: no back button", goos)
		}
		img := tr.Image()
		bg := systemTheme(false, ui.Preferences{}).Background
		r := e.Bounds
		edge := pixel(img, r.X+r.W/2, r.Y+1) // the face's top edge
		flat := colorNear(edge, bg, 6)
		if goos == "windows" && !flat {
			t.Errorf("windows: back button has a face (%v on %v)", edge, bg)
		}
		if goos == "darwin" && flat {
			t.Errorf("macOS: back button lost its push face")
		}
	}
}

// ui-review 2.3: busyButton, however a caller builds it, gives a disabled
// primary the disabled face, not a washed-out accent.
func TestBusyButtonDisabledPrimary(t *testing.T) {
	for _, goos := range []string{"windows", "darwin"} {
		usePlat(t, goos)
		for _, dark := range []bool{true, false} {
			tr := renderControls(t, dark, func(c *ui.Context) {
				ui.Row(c).Gap(12).Children(func() {
					busyButton(c.Key("on"), "BusyOn", true, false, false)
					busyButton(c.Key("off"), "BusyOff", true, false, true)
				})
			})
			img := tr.Image()
			th := systemTheme(dark, ui.Preferences{})
			at := func(label string) ui.Color {
				r, ok := tr.Find(label)
				if !ok {
					t.Fatalf("no %q", label)
				}
				return pixel(img, r.X+4, r.Y+r.H/2) // the button (it carries the label): inside its padding
			}
			if on := at("BusyOn"); !colorNear(on, th.Accent, 24) {
				t.Errorf("%s dark=%v: enabled primary %v, want the accent", goos, dark, on)
			}
			if off := at("BusyOff"); colorNear(off, th.Accent, 60) || colorNear(off, th.Accent.Mix(th.Background, 0.5), 30) {
				t.Errorf("%s dark=%v: disabled primary keeps the accent: %v", goos, dark, off)
			}
		}
	}
}
