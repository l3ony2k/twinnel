package main

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// The tray flyout stands flyoutEdge DIP inside its display's work area, as
// Windows 11's own flyouts do (review 5.1: it touched the screen's edge).
func TestFlyoutInset(t *testing.T) {
	// A 2560×1402 DIP work area over a taskbar (3840×2103 px at 150 %),
	// and a second display to its right.
	main := mygo.Display{Bounds: mygo.Rectangle{Width: 2560, Height: 1440}, WorkArea: mygo.Rectangle{Width: 2560, Height: 1402}}
	right := mygo.Display{Bounds: mygo.Rectangle{X: 2560, Width: 1920, Height: 1080}, WorkArea: mygo.Rectangle{X: 2560, Width: 1920, Height: 1032}}
	displays := []mygo.Display{main, right}
	for _, tc := range []struct {
		name string
		in   mygo.Rectangle
		want mygo.Rectangle
		ok   bool
	}{
		{"against the corner", mygo.Rectangle{X: 2220, Y: 1007, Width: 340, Height: 395}, mygo.Rectangle{X: 2208, Y: 995, Width: 340, Height: 395}, true},
		{"already inside", mygo.Rectangle{X: 2000, Y: 900, Width: 340, Height: 395}, mygo.Rectangle{X: 2000, Y: 900, Width: 340, Height: 395}, true},
		{"top-left taskbar", mygo.Rectangle{X: 0, Y: 0, Width: 340, Height: 395}, mygo.Rectangle{X: 12, Y: 12, Width: 340, Height: 395}, true},
		{"the second display", mygo.Rectangle{X: 4140, Y: 637, Width: 340, Height: 395}, mygo.Rectangle{X: 4128, Y: 625, Width: 340, Height: 395}, true},
		{"taller than the room", mygo.Rectangle{X: 2220, Y: 0, Width: 340, Height: 1402}, mygo.Rectangle{X: 2208, Y: 0, Width: 340, Height: 1402}, true},
		{"on no display", mygo.Rectangle{X: -900, Y: 0, Width: 340, Height: 395}, mygo.Rectangle{X: -900, Y: 0, Width: 340, Height: 395}, false},
	} {
		got, ok := flyoutInset(tc.in, displays, flyoutEdge)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: flyoutInset(%v) = %v, %v; want %v, %v", tc.name, tc.in, got, ok, tc.want, tc.ok)
		}
		if again, _ := flyoutInset(got, displays, flyoutEdge); again != got {
			t.Errorf("%s: not stable: %v then %v (the nudge would loop)", tc.name, got, again)
		}
	}
}

// panelTester renders the flyout on goos's UI path from the synthetic
// snapshot, edited by edit.
func panelTester(t *testing.T, goos string, edit func(*status.Snapshot)) *ui.Tester {
	t.Helper()
	old, oldT := plat, i18n.T
	t.Cleanup(func() { plat, i18n.T = old, oldT; resetFlags() })
	plat = platformFor(goos)
	resetFlags()
	i18n.Set("zh")
	a, _ := layoutApp(t)
	snap := a.shotSnap
	a.shotSnap = func(s *status.Snapshot) {
		snap(s)
		s.Helper.State = status.HelperConnected
		if edit != nil {
			edit(s)
		}
	}
	tr := ui.NewTesterWith(func(c *ui.Context) { a.panelView(c) }, panelWidth, 600, ui.TesterOptions{Dark: true})
	tr.Frame()
	tr.Frame()
	return tr
}

// On Windows the flyout uses Fluent's controls, not the menu bar extra's
// (review 5.2); macOS keeps its own.
func TestPanelControlsFollowPlatform(t *testing.T) {
	t.Run("windows", func(t *testing.T) {
		tr := panelTester(t, "windows", nil)
		T := i18n.T
		// TUN is a ToggleSwitch, as Tailscale's, not a round tile on a
		// row that toggles.
		if _, ok := tr.AccessByRole(ui.RoleSwitch, T.Tun); !ok {
			t.Errorf("TUN is not a switch: %v", tr.AccessAll())
		}
		// The mode is a SelectorBar: the choices as text, one chosen.
		var chosen []string
		for _, n := range tr.AccessAll() {
			for _, md := range []string{"rule", "global", "direct"} {
				if n.Name == T.ModeName(md) && (n.Selected || n.Checked) {
					chosen = append(chosen, n.Name)
				}
			}
		}
		if len(chosen) != 1 {
			t.Errorf("mode: chosen %q, want one", chosen)
		}
		// The foot: icon buttons for the window and 设置, 应急恢复;
		// 退出 is in the tray's right-click menu, and no ellipsis.
		for _, name := range []string{T.OpenMainWindow, T.PageSettings} {
			if n, ok := tr.Access(name); !ok || n.Role != ui.RoleButton {
				t.Errorf("foot: no %q button", name)
			}
		}
		if !tr.HasText(T.Recovery) {
			t.Errorf("foot: no %q", T.Recovery)
		}
		for _, name := range []string{T.PanelQuit, T.PanelOpen} {
			if _, ok := tr.Access(name); ok {
				t.Errorf("foot: %q is a macOS menu row", name)
			}
		}
	})
	t.Run("darwin", func(t *testing.T) {
		tr := panelTester(t, "darwin", nil)
		T := i18n.T
		for _, name := range []string{T.PanelQuit, T.PanelOpen} {
			if _, ok := tr.Access(name); !ok {
				t.Errorf("macOS foot: no %q row", name)
			}
		}
		if n, ok := tr.Access(T.Tun); !ok || n.Role != ui.RoleSwitch {
			t.Errorf("macOS: the TUN row is not the switch")
		}
	})
}

// The header's rates keep their places as the figures and units change
// every tick (review 5.6): each is right-aligned in a slot of its own.
func TestPanelRatesSteady(t *testing.T) {
	edges := func(down, up int64) (float32, float32) {
		tr := panelTester(t, "windows", func(s *status.Snapshot) {
			s.Mihomo.Traffic.Down, s.Mihomo.Traffic.Up = down, up
		})
		d, ok1 := tr.Find("↓ " + status.Rate(down))
		u, ok2 := tr.Find("↑ " + status.Rate(up))
		if !ok1 || !ok2 {
			t.Fatalf("rates not shown: %q", tr.Texts())
		}
		return d.X + d.W, u.X + u.W
	}
	d1, u1 := edges(82_000, 185)
	d2, u2 := edges(0, 1_048_000)
	const eps = 0.5
	if abs32(d1-d2) > eps || abs32(u1-u2) > eps {
		t.Errorf("the rates moved: ↓ ends at %.1f then %.1f, ↑ at %.1f then %.1f", d1, d2, u1, u2)
	}
}
