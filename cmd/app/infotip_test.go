package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// The ⓘ in real use: VoiceOver reads its name (关于 <subject>) and the
// tip as its description; Tab reaches it, Space and Return open the
// popover, Esc closes it; 了解更多 runs more and closes the popover.
func TestInfoTipKeyboardAndAccess(t *testing.T) {
	const body = "TUN carries all traffic."
	more := 0
	tr := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(20, 20).Children(func() {
			ui.Row(c).Gap(4).Children(func() {
				ui.Text(c, "TUN")
				infoTipAbout(c, "TUN", body, func() { more++ })
			})
		})
	}, 480, 320)
	name := i18n.F(i18n.T.InfoTipAbout, "TUN")
	info, ok := tr.Access(name)
	if !ok {
		t.Fatalf("no element named %q", name)
	}
	if info.Description != body {
		t.Fatalf("description %q, want the tip", info.Description)
	}
	if info.Expanded {
		t.Fatal("expanded before opening")
	}
	if info.Bounds.W != infoTipSize || info.Bounds.H != infoTipSize {
		t.Fatalf("ⓘ is %v×%v, want a %v pt circle", info.Bounds.W, info.Bounds.H, infoTipSize)
	}

	tr.Key(0, ui.KeyTab)
	if !tr.Focused(name) {
		t.Fatal("Tab does not reach the ⓘ")
	}
	for _, k := range []ui.Key{ui.KeySpace, ui.KeyEnter} {
		tr.Key(0, k)
		if !tr.HasText(body) {
			t.Fatalf("key %v does not open the tip", k)
		}
		if info, _ := tr.Access(name); !info.Expanded {
			t.Fatalf("key %v: the ⓘ is not expanded while open", k)
		}
		tr.Key(0, ui.KeyEscape)
		if tr.HasText(body) {
			t.Fatal("Esc does not close the tip")
		}
		if !tr.Focused(name) {
			t.Fatal("closing the tip loses the focus")
		}
	}

	if err := tr.Click(name); err != nil {
		t.Fatal(err)
	}
	if err := tr.Click(i18n.T.LearnMore); err != nil {
		t.Fatal(err)
	}
	if more != 1 || tr.HasText(body) {
		t.Fatalf("了解更多: more ran %d times, tip still open %v", more, tr.HasText(body))
	}
}
