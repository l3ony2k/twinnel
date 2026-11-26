package main

import (
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/status"
)

// levelColor is the color of a status level.
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

// dot draws a status dot.
func dot(c *ui.Context, col ui.Color, size float32) ui.Element {
	return ui.Box(c).Size(size, size).Radius(size / 2).Background(col).Shrink(0)
}

// kv draws a label and a value on one row.
func kv(c *ui.Context, label, value string) {
	t := c.Theme()
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).Width(78).Shrink(0)
		ui.Text(c, value).FontSize(13).Grow(1).SingleLine().Selectable()
	})
}

// kvWith draws a label and custom content on one row.
func kvWith(c *ui.Context, label string, body func()) {
	t := c.Theme()
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).Width(78).Shrink(0)
		ui.Row(c).Gap(6).AlignItems(ui.Center).Grow(1).Children(body)
	})
}

// sectionTitle draws a small heading with a status dot.
func sectionTitle(c *ui.Context, title string, col ui.Color, right string) {
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		dot(c, col, 8)
		ui.Text(c, title).FontSize(13).Bold()
		ui.Spacer(c)
		if right != "" {
			ui.Text(c, right).FontSize(12).TextColor(t.TextMuted)
		}
	})
}

// card is a bordered panel of the main window.
func card(c *ui.Context, title string, body func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Padding(16).Gap(10).Radius(10).Background(t.Surface).Border(1, t.Border).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(14).Bold()
		}
		body()
	})
}

// delayColor colors a delay: green, yellow, red, muted when unknown.
func delayColor(t *ui.Theme, ms int, tested bool) ui.Color {
	switch {
	case !tested:
		return t.TextMuted
	case ms <= 0:
		return t.Danger
	case ms < 300:
		return t.Success
	case ms < 800:
		return t.Warning
	}
	return t.Danger
}
