package main

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"
)

// The main window's source list, drawn by the app instead of ui.Sidebar so
// that it follows macOS's sidebar rules exactly (owner feedback):
//
//   - The selected row of the key window is filled with the system accent,
//     icon and label in AccentText; in a window that is not key it is a
//     clearly visible gray, icon and label in the text color. ui.Sidebar
//     shows the accent only while the list has the keyboard focus, and its
//     gray nearly vanished on the light sidebar.
//   - Hover is a faint fill, lighter than either selection.
//   - Every count is one component (countBadge): a pill of the same height,
//     right edge and tabular figures on every row, centered on the row.
//     Its tone tells neutral counts (devices online) from attention ones
//     (health warnings), never its shape or place.
//
// Up and Down move the selection while the list has the focus; each row is
// a tree item that reports itself selected to assistive technology.

// navTone is a count badge's tone.
type navTone int

const (
	toneNeutral   navTone = iota // a plain count
	toneAttention                // something to look at (warnings)
	toneAlert                    // something failed
)

// navItem is one row of the source list.
type navItem struct {
	id    string
	icon  *ui.SVG
	label string
	count int // shown when > 0
	tone  navTone
	digit int    // 1…9: the page's ⌘ digit (key), 0 for none
	key   ui.Key // the digit's key
}

// navSection is a titled group of rows; an empty title draws none.
type navSection struct {
	title string
	items []navItem
}

const (
	navRowHeight = 28 // macOS sidebar rows, medium size
	navBadgeH    = 18
)

// navList draws the sections and reports whether the selection changed.
// active is whether the window is key (c.WindowActive, or the shot
// override); hover forces a row's hover look in shots ("" = none).
func navList(c *ui.Context, selected *string, sections []navSection, active bool, hover string) bool {
	var order []string
	for _, s := range sections {
		for _, it := range s.items {
			order = append(order, it.id)
		}
	}
	changed := false
	list := ui.Column(c.Key("nav")).Gap(2).Focusable().Role(ui.RoleTree).Label("duet")
	list.Children(func() {
		for si, s := range sections {
			if s.title != "" {
				top := float32(14)
				if si == 0 {
					top = 0
				}
				ui.Text(c, s.title).FontSize(plat.smallSize).FontWeight(600).TextColor(c.Theme().TextMuted).
					Padding(top, 10, 4).SingleLine()
			}
			for _, it := range s.items {
				if navRow(c, it, *selected == it.id, active, hover == it.id) && *selected != it.id {
					*selected, changed = it.id, true
				}
			}
		}
	})
	step := 0
	if list.Shortcut(0, ui.KeyDown) {
		step = 1
	} else if list.Shortcut(0, ui.KeyUp) {
		step = -1
	}
	if step != 0 {
		if i := slices.Index(order, *selected); i >= 0 {
			if j := i + step; j >= 0 && j < len(order) {
				*selected, changed = order[j], true
			}
		}
	}
	return changed
}

// navRow draws one row and reports a click.
func navRow(c *ui.Context, it navItem, chosen, active, forceHover bool) bool {
	t := c.Theme()
	iconCol, textCol := t.Accent, t.Text
	row := ui.Row(c.Key("nav:" + it.id)).Height(navRowHeight).PaddingX(10).Gap(8).AlignItems(ui.Center).
		Radius(plat.controlRadius).Cursor(ui.CursorPointer).Role(ui.RoleTreeItem).Label(it.label).AccessSelected(chosen)
	if it.digit > 0 { // VoiceOver reads the row's ⌘ digit
		row.KeyShortcuts(ui.Cmd, it.key)
	}
	onAccent := false
	switch {
	case chosen && active:
		row.Background(t.Accent)
		iconCol, textCol, onAccent = t.AccentText, t.AccentText, true
	case chosen:
		row.Background(navInactiveFill(t))
		iconCol = t.Text
	case forceHover || row.Hovered():
		row.Background(navHoverFill(t))
	}
	row.Children(func() {
		ui.Icon(c, it.icon).FontSize(iconNav).TextColor(iconCol).Width(18).Shrink(0)
		ui.Text(c, it.label).TextColor(textCol).Grow(1).Shrink(1).SingleLine()
		if it.count > 0 {
			countBadge(c, it.count, it.tone, onAccent)
		}
	})
	return row.Clicked()
}

// navInactiveFill is the selection of a window that is not key: a gray
// that stands out on the sidebar in both appearances.
func navInactiveFill(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(255, 255, 255, 0.16)
	}
	return ui.RGBA(0, 0, 0, 0.13)
}

// navHoverFill is fainter than either selection.
func navHoverFill(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(255, 255, 255, 0.07)
	}
	return ui.RGBA(0, 0, 0, 0.05)
}

// countBadge is the one count component: a pill of fixed height, at least
// as wide as it is tall, its figures tabular and centered in it. The tone
// sets its colors; on the accent-filled row it inverts.
func countBadge(c *ui.Context, n int, tone navTone, onAccent bool) ui.Element {
	t := c.Theme()
	p := pal(t)
	var bg, fg ui.Color
	switch {
	case onAccent && tone == toneNeutral:
		bg, fg = t.AccentText.Alpha(0.24), t.AccentText
	case onAccent:
		bg, fg = t.AccentText, t.Accent // inverted: the attention stays visible
	case tone == toneAttention:
		bg, fg = t.Warning.Alpha(0.22), p.warnText
	case tone == toneAlert:
		bg, fg = t.Danger.Alpha(0.2), p.badText
	default:
		bg, fg = t.Text.Alpha(0.09), t.TextMuted
	}
	return ui.Row(c).Height(navBadgeH).MinWidth(navBadgeH).PaddingX(6).Radius(navBadgeH / 2).
		Background(bg).AlignItems(ui.Center).Justify(ui.Center).Shrink(0).Children(func() {
		num(ui.Text(c, fmt.Sprint(n)).FontSize(plat.smallSize).FontWeight(600).TextColor(fg).SingleLine())
	})
}
