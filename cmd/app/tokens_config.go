package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Motion of the config-side pages (subscriptions, layers, takeover plans,
// history). Kept here, not in widgets.go, while widgets.go is being
// reworked in parallel; fold into its easing tokens when they land.

// easeInOutConfig is the curve of a size or place change on screen
// (review-a 011: cubic-bezier(.42, 0, .58, 1)).
var easeInOutConfig = ui.CubicBezier(.42, 0, .58, 1)

// rowMotion is how a list row comes and goes, as NSTableView's
// removeRows(withAnimation: [.effectFade, .slideUp]) does: it fades in
// while its height opens, and out (a touch smaller) while the rows below
// close the gap, in 220 ms. It also carries the row's own height change
// as it discloses its editor. Reduce Motion makes it instant (MyGo).
var rowMotion = ui.ElementTransition{
	Duration: 220 * time.Millisecond,
	Ease:     easeInOutConfig,
	Enter:    &ui.Motion{Opacity: 0, Collapse: true},
	Exit:     &ui.Motion{Opacity: 0, Scale: 0.96, Collapse: true},
}

// chevronMotion turns a disclosure chevron with its row, as
// NSOutlineView's triangle turns (review-b 015 #3).
var chevronMotion = ui.ElementTransition{Duration: 200 * time.Millisecond, Ease: ui.CubicBezier(.22, 1, .36, 1)}

// disclosureChevron is a chevron-right turned down while open.
func disclosureChevron(c *ui.Context, open bool) ui.Element {
	ch := ui.Icon(c, iconCollapsed).FontSize(12).TextColor(c.Theme().TextMuted).Width(14).Shrink(0).
		Transition(chevronMotion).AccessHidden(true)
	if open {
		ch.Rotate(90)
	}
	return ch
}
