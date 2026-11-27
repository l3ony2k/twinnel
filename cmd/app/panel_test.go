package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// A flyout row that toggles as a whole (TUN) holds an ⓘ: clicking the ⓘ
// opens its tip and leaves the row alone; clicking the row toggles it.
func TestPanelRowInfoTipDoesNotToggle(t *testing.T) {
	on, toggles := false, 0
	tr := ui.NewTester(func(c *ui.Context) {
		row := panelRow(c.Key("tun"), "TUN").Role(ui.RoleSwitch).Checked(on)
		row.Children(func() {
			ui.Text(c, "TUN")
			infoTip(c, "", "what TUN does", nil)
			ui.Text(c, "utun9").Grow(1)
		})
		if row.Clicked() {
			on = !on
			toggles++
		}
	}, 320, 80)
	if err := tr.Click(" what TUN does"); err != nil {
		t.Fatal(err)
	}
	if toggles != 0 {
		t.Fatalf("clicking the ⓘ toggled the row %d times", toggles)
	}
	if err := tr.Click("utun9"); err != nil {
		t.Fatal(err)
	}
	if toggles != 1 || !on {
		t.Fatalf("clicking the row: toggles=%d on=%v, want 1 true", toggles, on)
	}
}
