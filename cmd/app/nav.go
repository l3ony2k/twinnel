package main

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"
)

// The main window's source list, drawn by the app instead of ui.Sidebar so
// that it follows the platform's sidebar rules exactly (plat.navSel):
//
//   - macOS 26 and later (Liquid Glass, as Finder): the selected row is a
//     neutral translucent fill, the label keeps the text color and the
//     icon keeps the accent, as every row's icon does; a hovered row does
//     not change. Rows are Finder's: 32 pt, no gap, the fill inset 10 pt
//     from the sidebar's edges with radius 8. In a window that is not key
//     the fill stays, the icons turn gray.
//   - macOS 15 and earlier: the selected row of the key window is filled
//     with the accent, icon and label in AccentText; in a window that is
//     not key it is gray; hover is a faint fill.
//   - Windows 11 (NavigationView): the neutral fill, a short accent pill at
//     the row's leading edge, a hover fill. GNOME: the neutral fill.
//   - Every count is one component (countBadge) at the row's right edge,
//     tabular figures, centered on the row. A neutral count (devices
//     online) is the plain number in the secondary color, as Finder's and
//     Mail's on macOS 26; one that calls for attention (health warnings,
//     failures) is a tinted pill. On the rail only those show (railMark).
//
// Up and Down move the selection while the list has the focus; each row is
// a tree item that reports itself selected to assistive technology.
// See scratch/sidebar/spec.md for the measurements and sources.

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

const navBadgeH = 18

// navMetrics are the list's row height, the gap between rows and the
// icon's size: Finder's on macOS 26+ (32 pt rows, touching, 17–19 pt
// symbols), the medium sidebar size before.
func navMetrics() (rowH, gap, icon float32) {
	if plat.navSel == navNeutral && liquidGlass {
		return 32, 0, iconSidebar
	}
	return 28, 2, iconNav
}

// navList draws the sections and reports whether the selection changed.
// With reselect, a click on the selected row reports a change too (a
// sub-page of it shows: the click goes back to the row's page).
// active is whether the window is key (c.WindowActive, or the shot
// override); hover forces a row's hover look in shots ("" = none).
func navList(c *ui.Context, selected *string, sections []navSection, active bool, hover string, reselect bool) bool {
	var order []string
	for _, s := range sections {
		for _, it := range s.items {
			order = append(order, it.id)
		}
	}
	changed := false
	_, gap, _ := navMetrics()
	list := ui.Column(c.Key("nav")).Gap(gap).Focusable().Role(ui.RoleTree).Label("Twinnel")
	list.Children(func() {
		for si, s := range sections {
			if s.title != "" {
				if navRail { // icons only (sidebar.go)
					railGap(c)
				} else {
					navHeader(c, s.title, si == 0)
				}
			}
			for _, it := range s.items {
				row := navRow
				if navRail {
					row = func(c *ui.Context, it navItem, chosen, active, _ bool) bool { return railRow(c, it, chosen, active) }
				}
				if row(c, it, *selected == it.id, active, hover == it.id) && (*selected != it.id || reselect) {
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

// navHeader is a section's title. On macOS 26+ it is Finder's: a row of
// the list's height, its text 11 pt semibold at the row's foot, 7 pt in
// from the selection's edge, in the sidebar's faint label color.
func navHeader(c *ui.Context, title string, first bool) {
	t := c.Theme()
	if plat.navSel == navNeutral && liquidGlass {
		rowH, _, _ := navMetrics()
		h := rowH
		if first {
			h = 0
		}
		ui.Column(c).MinHeight(h).Justify(ui.End).Padding(0, 7, 5).Children(func() {
			ui.Text(c, title).FontSize(plat.smallSize).FontWeight(600).TextColor(navHeaderColor(t)).SingleLine()
		})
		return
	}
	top := float32(14)
	if first {
		top = 0
	}
	ui.Text(c, title).FontSize(plat.smallSize).FontWeight(600).TextColor(t.TextMuted).
		Padding(top, 10, 4).SingleLine()
}

// navGap is the gap from a row's icon to its label; the sidebar's foot
// uses it too (footMark).
const navGap = 8

// navRowPadX is a row's inset from the selection's edge to its icon:
// Finder's 9 on macOS 26+, 10 before.
func navRowPadX() float32 {
	if plat.navSel == navNeutral {
		return 9
	}
	return 10
}

// navRow draws one row and reports a click.
func navRow(c *ui.Context, it navItem, chosen, active, forceHover bool) bool {
	t := c.Theme()
	rowH, _, iconSize := navMetrics()
	iconCol, textCol := t.Accent, t.Text
	row := ui.Row(c.Key("nav:" + it.id)).Height(rowH).Gap(navGap).AlignItems(ui.Center).
		Role(ui.RoleTreeItem).Label(it.label).AccessSelected(chosen)
	if it.digit > 0 { // VoiceOver reads the row's ⌘ digit
		row.KeyShortcuts(ui.Cmd, it.key)
	}
	onAccent, pill := false, false
	switch plat.navSel {
	case navNeutral:
		// Finder: icon at 9 pt, label at 36 pt from the fill's edge.
		row.PaddingX(navRowPadX()).Radius(navSelRadius)
		if chosen {
			row.Background(navNeutralFill(t, active))
		}
		if !active && liquidGlass {
			iconCol = t.TextMuted
		}
	case navNeutralPill:
		row.PaddingX(navRowPadX()).Radius(plat.controlRadius)
		switch {
		case chosen:
			row.Background(navNeutralFill(t, true))
			pill = active
		case forceHover || row.Hovered():
			row.Background(navHoverFill(t))
		}
	default: // navAccentFill
		row.PaddingX(navRowPadX()).Radius(plat.controlRadius)
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
	}
	row.Children(func() {
		if pill { // Windows 11: 3 × 16, at the row's leading edge
			ui.Box(c).Absolute().Left(0).Top((rowH-16)/2).Size(3, 16).Radius(1.5).Background(t.Accent).AccessHidden(true)
		}
		ui.Icon(c, it.icon).FontSize(iconSize).TextColor(iconCol).Width(iconSize + 2).Shrink(0)
		ui.Text(c, it.label).TextColor(textCol).Grow(1).Shrink(1).SingleLine()
		if it.count > 0 {
			countBadge(c, it.count, it.tone, onAccent)
		}
	})
	return row.Clicked()
}

// navSelRadius is the neutral selection's corner radius (Finder, macOS
// 26+: about 8 pt on a 32 pt row).
const navSelRadius = 8

// navNeutralFill is the selection on macOS 26+ (and Windows 11, GNOME): a
// neutral translucent gray over the sidebar material, not the accent.
// Measured on Finder (dark): white at about 14%. A window that is not key
// keeps a lighter one.
func navNeutralFill(t *ui.Theme, active bool) ui.Color {
	if t.Dark {
		if active {
			return ui.RGBA(255, 255, 255, 0.14)
		}
		return ui.RGBA(255, 255, 255, 0.10)
	}
	if active {
		return ui.RGBA(0, 0, 0, 0.09)
	}
	return ui.RGBA(0, 0, 0, 0.06)
}

// navHeaderColor is a section title's color on macOS 26+: fainter than
// TextMuted, as Finder's (white at about 37% in dark).
func navHeaderColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(255, 255, 255, 0.37)
	}
	return ui.RGBA(0, 0, 0, 0.36)
}

// navInactiveFill is the accent style's selection of a window that is
// not key: a gray that stands out on the sidebar in both appearances.
func navInactiveFill(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(255, 255, 255, 0.16)
	}
	return ui.RGBA(0, 0, 0, 0.13)
}

// navHoverFill is fainter than either selection (not used on macOS 26+,
// where hovering a sidebar row shows nothing).
func navHoverFill(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(255, 255, 255, 0.07)
	}
	return ui.RGBA(0, 0, 0, 0.05)
}

// countBadge is the one count component. A neutral count is the plain
// number, 11 pt, tabular, in the secondary color (AccentText on the
// accent-filled row). An attention or alert count is a pill of fixed
// height, at least as wide as it is tall, its figures centered in it;
// the tone sets its colors, and on the accent-filled row it inverts.
func countBadge(c *ui.Context, n int, tone navTone, onAccent bool) ui.Element {
	t := c.Theme()
	p := pal(t)
	if tone == toneNeutral {
		fg := t.TextMuted
		if onAccent {
			fg = t.AccentText
		}
		return ui.Row(c).Height(navBadgeH).MinWidth(navBadgeH).AlignItems(ui.Center).Justify(ui.End).Shrink(0).Children(func() {
			num(ui.Text(c, fmt.Sprint(n)).FontSize(plat.smallSize).TextColor(fg).SingleLine())
		})
	}
	var bg, fg ui.Color
	switch {
	case onAccent:
		bg, fg = t.AccentText, t.Accent // inverted: the attention stays visible
	case tone == toneAttention:
		bg, fg = t.Warning.Alpha(0.22), p.warnText
	default: // toneAlert
		bg, fg = t.Danger.Alpha(0.2), p.badText
	}
	return ui.Row(c).Height(navBadgeH).MinWidth(navBadgeH).PaddingX(6).Radius(navBadgeH / 2).
		Background(bg).AlignItems(ui.Center).Justify(ui.Center).Shrink(0).Children(func() {
		num(ui.Text(c, fmt.Sprint(n)).FontSize(plat.smallSize).FontWeight(600).TextColor(fg).SingleLine())
	})
}
