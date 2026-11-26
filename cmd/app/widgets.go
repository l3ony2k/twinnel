package main

import (
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/status"
)

// The app's design tokens and shared pieces. Every page builds from these
// so sizes, colors and type stay one system (diagnosis: "design system
// pass"). Platform values come from plat (platform.go).
//
// Text roles: primary (t.Text), secondary (t.TextMuted), and status text
// (pal.okText / warnText / badText), which is darker than the dot colors in
// the light appearance so it stays readable.

// palette is what the theme does not name.
type palette struct {
	group      ui.Color // fill of a grouped box
	groupLine  ui.Color // its hairline
	separator  ui.Color // lines inside a group
	okText     ui.Color
	warnText   ui.Color
	badText    ui.Color
	accentSoft ui.Color // selected chip / row fill
	code       ui.Color // code and log backgrounds
}

func pal(t *ui.Theme) palette {
	if t.Dark {
		return palette{
			group:      ui.RGBA(255, 255, 255, 0.045),
			groupLine:  ui.RGBA(255, 255, 255, 0.07),
			separator:  ui.RGBA(255, 255, 255, 0.08),
			okText:     t.Success,
			warnText:   t.Warning,
			badText:    t.Danger,
			accentSoft: t.Accent.Alpha(0.22),
			code:       ui.RGBA(0, 0, 0, 0.25),
		}
	}
	return palette{
		group:      ui.Hex("#ffffff"),
		groupLine:  ui.RGBA(0, 0, 0, 0.07),
		separator:  ui.RGBA(0, 0, 0, 0.08),
		okText:     ui.Hex("#248a3d"),
		warnText:   ui.Hex("#c93400"),
		badText:    ui.Hex("#d70015"),
		accentSoft: t.Accent.Alpha(0.12),
		code:       ui.RGBA(0, 0, 0, 0.035),
	}
}

// Spacing steps (4 px grid).
const (
	gapRow     = 10 // between rows inside a group
	gapGroup   = 20 // between groups on a page
	groupPadX  = 16
	groupPadY  = 12
	labelWidth = 96 // the label column of kv rows
)

// levelColor is the dot color of a status level.
func levelColor(t *ui.Theme, l status.Level) ui.Color {
	switch l {
	case status.LevelOK:
		return t.Success
	case status.LevelWarn:
		return t.Warning
	case status.LevelError:
		return t.Danger
	}
	return t.TextMuted
}

// levelText is the text color of a status level.
func levelText(t *ui.Theme, l status.Level) ui.Color {
	p := pal(t)
	switch l {
	case status.LevelWarn:
		return p.warnText
	case status.LevelError:
		return p.badText
	}
	return t.Text
}

// dot draws a status dot.
func dot(c *ui.Context, col ui.Color, size float32) ui.Element {
	return ui.Box(c).Size(size, size).Radius(size / 2).Background(col).Shrink(0)
}

// num styles a figure that changes in place: digits of one width.
func num(e ui.Element) ui.Element { return e.FontFeatures("tnum") }

// small draws secondary text.
func small(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(plat.bodySize - 1).TextColor(c.Theme().TextMuted)
}

// kv draws a label and a value on one row.
func kv(c *ui.Context, label, value string) {
	kvWith(c, label, func() {
		num(ui.Text(c, value).Grow(1).SingleLine().Selectable())
	})
}

// kvWith draws a label and custom content on one row.
func kvWith(c *ui.Context, label string, body func()) {
	t := c.Theme()
	ui.Row(c).Gap(12).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Children(func() {
		ui.Text(c, label).TextColor(t.TextMuted).Width(labelWidth).Shrink(0).SingleLine()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Children(body)
	})
}

// sectionTitle draws a heading with a status dot and a state on the right.
func sectionTitle(c *ui.Context, title string, col ui.Color, right string) {
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		dot(c, col, 8)
		ui.Text(c, title).FontWeight(plat.headingWeight)
		ui.Spacer(c)
		if right != "" {
			num(ui.Text(c, right).TextColor(t.TextMuted).SingleLine())
		}
	})
}

// card is a group of the main window, as System Settings draws them: the
// heading above a quiet box. An empty title draws only the box.
func card(c *ui.Context, title string, body func()) ui.Element {
	return cardWith(c, title, nil, body)
}

// cardWith is card with something at the right of the heading (a button,
// a count).
func cardWith(c *ui.Context, title string, trailing func(), body func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(8).Children(func() {
		if title != "" || trailing != nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(4).Height(28).Children(func() {
				ui.Text(c, title).FontSize(plat.headingSize).FontWeight(plat.headingWeight).SingleLine()
				ui.Spacer(c)
				if trailing != nil {
					trailing()
				}
			})
		}
		groupBox(c, t).Children(body)
	})
}

// headedCard is card with a heading row the caller builds (title, hints,
// buttons), above the box.
func headedCard(c *ui.Context, head func(), body func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(4).MinHeight(28).Children(head)
		groupBox(c, t).Children(body)
	})
}

// groupBox is the box of a group, without heading.
func groupBox(c *ui.Context, t *ui.Theme) ui.Element {
	p := pal(t)
	return ui.Column(c).Padding(groupPadY, groupPadX).Gap(gapRow).Radius(plat.groupRadius).
		Background(p.group).Border(1, p.groupLine)
}

// pageHeader draws a page's title with actions at the right.
func pageHeader(c *ui.Context, title string, trailing func()) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(32).DragWindow().Children(func() {
		ui.Text(c, title).FontSize(plat.titleSize).FontWeight(plat.titleWeight).SingleLine()
		ui.Spacer(c)
		if trailing != nil {
			trailing()
		}
	})
}

// meta draws a quiet label: a type, a role, a count. Badges are for
// things that call for attention, not for metadata.
func meta(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(plat.smallSize).TextColor(c.Theme().TextMuted).SingleLine().Shrink(0)
}

// tag draws a small outlined label for a state worth noticing (使用中,
// 托管): quieter than a filled badge.
func tag(c *ui.Context, s string) ui.Element {
	t := c.Theme()
	return ui.Row(c).Padding(1, 6).Radius(4).Border(1, t.Border).Shrink(0).Children(func() {
		ui.Text(c, s).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine()
	})
}

// notice is one line of information with an icon, for page-level states
// such as read-only mode. Errors use errorNote.
func notice(c *ui.Context, icon *ui.SVG, text string) ui.Element {
	t := c.Theme()
	return ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(8, 12).Radius(plat.controlRadius+2).
		Background(pal(t).group).Border(1, pal(t).groupLine).Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(t.TextMuted)
		ui.Text(c, text).FontSize(plat.bodySize - 1).TextColor(t.TextMuted).Grow(1).Wrap()
	})
}

// errorNote shows what failed, next to where it failed.
func errorNote(c *ui.Context, text string) ui.Element {
	t := c.Theme()
	return ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, iconError).FontSize(14).TextColor(t.Danger).MarginY(1)
		ui.Text(c, text).FontSize(plat.bodySize - 1).TextColor(pal(t).badText).Grow(1).Wrap().Selectable()
	})
}

// emptyState says why a list is empty and what to do about it.
func emptyState(c *ui.Context, icon *ui.SVG, title, hint string) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(6).AlignItems(ui.Center).Padding(28, 16).Children(func() {
		ui.Icon(c, icon).FontSize(22).TextColor(t.TextMuted)
		ui.Text(c, title).FontWeight(500)
		if hint != "" {
			ui.Text(c, hint).FontSize(plat.bodySize - 1).TextColor(t.TextMuted).TextAlign(ui.Center).Wrap().MaxWidth(420)
		}
	})
}

// delayColor colors a delay: plain while fast, orange when slow, red when
// it failed or is very slow. Green would be on every row and mean nothing.
func delayColor(t *ui.Theme, ms int, tested bool) ui.Color {
	p := pal(t)
	switch {
	case !tested:
		return t.TextMuted
	case ms <= 0:
		return p.badText
	case ms < 300:
		return t.TextMuted
	case ms < 800:
		return p.warnText
	}
	return p.badText
}
