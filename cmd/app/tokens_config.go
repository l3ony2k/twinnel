package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Motion of the config-side list rows (subscriptions, layers, takeover
// plans), built on widgets.go's easing tokens.

// rowMotion is how a list row comes and goes, as NSTableView's
// removeRows(withAnimation: [.effectFade, .slideUp]) does: it fades in
// while its height opens, and out (a touch smaller) while the rows below
// close the gap, in 220 ms (review-b 014). It also carries the row's own
// height change as it discloses its editor. Reduce Motion makes it
// instant (MyGo).
var rowMotion = ui.ElementTransition{
	Duration: 220 * time.Millisecond,
	Ease:     easeInOut,
	Enter:    &ui.Motion{Opacity: 0, Collapse: true},
	Exit:     &ui.Motion{Opacity: 0, Scale: 0.96, Collapse: true},
}

// rowMove is how list rows glide to new places after a drag or a move.
var rowMove = ui.Animation{Duration: 220 * time.Millisecond, Ease: easeInOut}
