package main

import (
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// infoTip is the small ⓘ after a label that explains one feature in a
// popover: what it is, what it changes, when to use it. It is the place for
// the sentence the copy rule keeps out of the page itself (AGENTS.md):
// the page shows state and controls, the ⓘ says what they mean.
//
// The button is faceless (muted glyph, a round faint fill on hover or
// while open), sized to sit on a text line; a click, Space or Return opens
// the popover, a click outside or Esc closes it. title is optional and
// shows bold in the popover; body is one to three short sentences. more,
// when not nil, adds a link-styled 了解更多 that runs it (for example a
// jump to the 关于 page), closing the popover first.
func infoTip(c *ui.Context, title, body string, more func()) ui.Element {
	return infoTipFor(c, title, title, body, more)
}

// infoTipAbout is infoTip for a label right before it: the popover has no
// title of its own (the label is beside it), and subject names the button
// for VoiceOver (关于 TUN).
func infoTipAbout(c *ui.Context, subject, body string, more func()) ui.Element {
	return infoTipFor(c, subject, "", body, more)
}

// infoTipSize is the ⓘ button's side: the inline icon and a 2 pt ring, so
// the hover fill is a circle around the glyph.
const infoTipSize = iconInline + 4

// shotTipOpen, in shot mode, opens the ⓘ whose body it is (shots_infotips.go).
var shotTipOpen string

func infoTipFor(c *ui.Context, subject, title, body string, more func()) ui.Element {
	t := c.Theme()
	T := i18n.T
	label := T.InfoTipLabel
	if subject != "" {
		label = i18n.F(T.InfoTipAbout, subject)
	}
	b := ui.Button(c.Key("infotip:"+title+body), "").Label(label).Description(body).HasPopup(ui.PopupDialog)
	open := ui.Local(b, "open", func() bool { return false })
	if shotTipOpen != "" && shotTipOpen == body {
		*open = true
	}
	b.Size(infoTipSize, infoTipSize).Padding(0, 0).Radius(infoTipSize/2).Shrink(0).
		Background(ui.Transparent).Border(0, ui.Transparent).Expanded(*open)
	if b.Hovered() || *open {
		b.Background(t.Text.Alpha(0.08))
	}
	b.Children(func() {
		ui.Icon(c, iconAbout).FontSize(iconInline).TextColor(t.TextMuted)
	})
	if b.Clicked() {
		*open = !*open
	}
	// The panel grows from the ⓘ and flips with it (popoverMotion); its
	// look stays MyGo's popover panel (radius 8, padding 4 + 4).
	panel := ui.Popover(c, b, open, func() {
		ui.Column(c).Gap(6).MaxWidth(300).Padding(4, 4).Children(func() {
			if title != "" {
				ui.Text(c, title).Bold().Wrap()
			}
			ui.Text(c, body).FontSize(plat.calloutSize).Wrap().LineHeight(lineText)
			if more != nil {
				if ui.Button(c.Key("infotip-more"), "").Background(ui.Transparent).Border(0, ui.Transparent).Padding(0, 0).AlignSelf(ui.Start).
					Children(func() {
						ui.Text(c, i18n.T.LearnMore).FontSize(plat.calloutSize).TextColor(t.Accent)
					}).Clicked() {
					*open = false
					more()
				}
			}
		})
	})
	if panel != (ui.Element{}) {
		panel.Transition(popoverMotion).AttachedOrigin()
	}
	return b
}
