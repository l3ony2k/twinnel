package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// MyGo pops a native menu up at its element's bottom-left corner, and
// AppKit puts the menu's top edge menuInsetTop above that point. The
// element carrying the menu therefore reaches plat.menuDrop below what it
// shows (menuBelow), so that the menu opens menuGap under the face, as a
// pull-down's does. These check that the host is that much taller than
// the face, that nothing around it moves, and that the strip under the
// face does not take the pointer.

// menuProbe lays out above, the control, then a text below it, in a
// window of the given width.
func menuProbe(control func(c *ui.Context)) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(20).Gap(0).AlignItems(ui.Start).Children(func() {
			control(c)
			ui.Text(c, "below")
		})
	}, 480, 320)
}

func TestPushMenuOpensBelowItsFace(t *testing.T) {
	const label = "Actions"
	menu := menuProbe(func(c *ui.Context) {
		pushMenu(c, label, func(m *ui.Menu) { m.Item("Restart") })
	})
	plain := menuProbe(func(c *ui.Context) { pushButton(c, label) })

	host, ok := menu.AccessByRole(ui.RoleMenuButton, label)
	if !ok {
		t.Fatalf("no menu button named %q", label)
	}
	btn, ok := plain.AccessByRole(ui.RoleButton, label)
	if !ok {
		t.Fatalf("no button named %q", label)
	}
	if host.Bounds.X != btn.Bounds.X || host.Bounds.Y != btn.Bounds.Y || host.Bounds.W < btn.Bounds.W {
		t.Fatalf("menu button at %v, a push button at %v: the face moved", host.Bounds, btn.Bounds)
	}
	// The anchor (the host's bottom edge) is menuDrop under the face.
	if got, want := host.Bounds.H, btn.Bounds.H+plat.menuDrop; got != want {
		t.Fatalf("host is %v tall, want the face (%v) + menuDrop (%v)", got, btn.Bounds.H, plat.menuDrop)
	}
	below, _ := menu.Find("below")
	belowPlain, _ := plain.Find("below")
	if below.Y != belowPlain.Y {
		t.Fatalf("text under the menu button at y=%v, under a push button at y=%v: the layout moved", below.Y, belowPlain.Y)
	}

	// A press on the face opens the menu; one in the strip under it does not.
	b := host.Bounds
	menu.ClickAt(b.X+b.W/2, b.Y+btn.Bounds.H/2)
	if len(menu.Menu()) == 0 {
		t.Fatal("pressing the face does not open the menu")
	}
	menu.CloseMenu()
	if plat.menuDrop > 0 {
		menu.ClickAt(b.X+b.W/2, b.Y+b.H-plat.menuDrop/2)
		if len(menu.Menu()) != 0 {
			t.Fatal("the strip under the face opens the menu")
		}
	}
}

func TestPanelMenuRowOpensBelowIt(t *testing.T) {
	const label = "Exit node"
	menu := menuProbe(func(c *ui.Context) {
		panelMenuRow(c, label, false, func(m *ui.Menu) { m.Item("None") }).Children(func() {
			ui.Text(c, label)
		})
	})
	host, ok := menu.AccessByRole(ui.RoleMenuButton, label)
	if !ok {
		t.Fatalf("no menu row named %q", label)
	}
	if got, want := host.Bounds.H, panelRowH+plat.menuDrop; got != want {
		t.Fatalf("row host is %v tall, want the row (%v) + menuDrop (%v)", got, panelRowH, plat.menuDrop)
	}
	below, _ := menu.Find("below")
	if want := host.Bounds.Y + panelRowH; below.Y < want-0.5 || below.Y > want+0.5 {
		t.Fatalf("text under the row at y=%v, want %v: the layout moved", below.Y, want)
	}
	b := host.Bounds
	menu.ClickAt(b.X+b.W/2, b.Y+panelRowH/2)
	if len(menu.Menu()) == 0 {
		t.Fatal("pressing the row does not open the menu")
	}
	menu.CloseMenu()
	if plat.menuDrop > 0 {
		menu.ClickAt(b.X+b.W/2, b.Y+b.H-plat.menuDrop/2)
		if len(menu.Menu()) != 0 {
			t.Fatal("the strip under the row opens the menu")
		}
	}
}
