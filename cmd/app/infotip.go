package main

import (
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
)

// infoTip is the small ⓘ after a label that explains one feature in a
// popover: what it is, what it changes, when to use it. It is the place for
// the sentence the copy rule keeps out of the page itself (AGENTS.md):
// the page shows state and controls, the ⓘ says what they mean.
//
// The button is faceless (muted glyph, faint fill on hover), sized to sit
// on a text line; a click or Space opens the popover, a click outside or
// Esc closes it. title is optional; body is one to three short sentences.
// more, when not nil, adds a link-styled 了解更多 that runs it (for
// example a jump to the 关于 page), closing the popover first.
func infoTip(c *ui.Context, title, body string, more func()) ui.Element {
	t := c.Theme()
	b := ui.Button(c.Key("infotip:"+title+body), "").Label(title + " " + body)
	open := ui.Local(b, "open", func() bool { return false })
	b.Padding(2, 2).Radius(plat.controlRadius).Background(ui.Transparent).Border(0, ui.Transparent)
	if b.Hovered() || *open {
		b.Background(t.Text.Alpha(0.06))
	}
	b.Children(func() {
		ui.Icon(c, iconAbout).FontSize(iconInline).TextColor(t.TextMuted)
	})
	if b.Clicked() {
		*open = !*open
	}
	ui.Popover(c, b, open, func() {
		ui.Column(c).Gap(6).MaxWidth(300).Padding(4, 4).Children(func() {
			if title != "" {
				ui.Text(c, title).Bold().Wrap()
			}
			ui.Text(c, body).FontSize(plat.calloutSize).Wrap().LineHeight(lineText)
			if more != nil {
				if ui.Button(c.Key("infotip-more"), "").Background(ui.Transparent).Border(0, ui.Transparent).Padding(0, 0).
					Children(func() {
						ui.Text(c, i18n.T.LearnMore).FontSize(plat.calloutSize).TextColor(t.Accent)
					}).Clicked() {
					*open = false
					more()
				}
			}
		})
	})
	return b
}
