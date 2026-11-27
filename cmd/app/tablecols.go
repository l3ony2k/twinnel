package main

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// Table columns and cells sized to what they hold. The rule (owner,
// 2026-11-27): one column of a table flexes, the one holding names; every
// other column is as wide as its widest value or its title, whichever is
// wider, so that no column clips its header and an address is never cut.
// A name that must shrink loses its middle ("build-run…ffice"), keeping
// its head and its tail, and shows whole on hover.

// widestIPv4 is the widest an IPv4 address can be: four groups of three
// digits, which mono with tabular digits draws all as wide.
const widestIPv4 = "255.255.255.255"

// monoSpan measures text as mono (widgets.go) draws it; the zero Span,
// as a table's plain cells draw it.
func monoSpan() ui.Span { return ui.Span{Font: "monospace", Size: plat.calloutSize, Features: "tnum"} }

// textWidth is the width s takes drawn as sp says, rounded up.
func textWidth(c *ui.Context, sp ui.Span, s string) float32 {
	sp.Text = s
	w, _ := c.MeasureText(0, sp)
	return float32(math.Ceil(float64(w)))
}

// tableCellPadX is the room a table cell leaves on either side of its
// content (MyGo's table: Space(2.5) each side).
func tableCellPadX(c *ui.Context) float32 { return 2 * c.Theme().Space(2.5) }

// fitColumn is a column as wide as the widest of values drawn as sp, plus
// extra (a mark before the text), or its title, whichever is wider. It is
// Fixed: it keeps its place and width, and draws no resize edge.
func fitColumn(c *ui.Context, title string, sp ui.Span, extra float32, values ...string) ui.TableColumn {
	w := textWidth(c, ui.Span{Weight: 600}, title) // the header's face
	for _, v := range values {
		w = max(w, extra+textWidth(c, sp, v))
	}
	// One DIP over the measure, so that rounding never ellipsizes the
	// widest value.
	return ui.TableColumn{Title: title, Width: w + tableCellPadX(c) + 1, Fixed: true}
}

// nameColumn is the column that takes the room the others leave: at
// least its title and about a dozen characters, then its names lose
// their middle (middleText). Fixed, so its edge does not offer to resize
// what only the window's width decides.
func nameColumn(c *ui.Context, title string, minW float32) ui.TableColumn {
	w := textWidth(c, ui.Span{Weight: 600}, title) + tableCellPadX(c) + 1
	return ui.TableColumn{Title: title, MinWidth: max(w, minW), Fixed: true}
}

// middleText is a name, a path or an address on one line that, when it
// does not fit, loses its middle rather than its end and shows whole on
// hover. It shrinks to whatever room its row leaves.
func middleText(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).SingleLine().TruncateMiddle().Shrink(1).MinWidth(0).Tooltip(s)
}
