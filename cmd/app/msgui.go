package main

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/msg"
	"github.com/l3ony2k/duet/core/rpc"
)

// Helper messages on screen (design review U57): a coded message in the
// UI language; one duet has no words for shows a generic line, and the
// helper's own text stays behind 复制原文 and the tooltip, never as the
// line itself.

// msgNote draws a coded helper message as a warning or an error line.
func msgNote(c *ui.Context, m rpc.Msg, warning bool) ui.Element {
	t := c.Theme()
	text, raw := i18n.T.MsgText(m, warning)
	icon, col, txt := iconError, t.Danger, pal(t).badText
	if warning {
		icon, col, txt = iconWarning, pal(t).warnText, t.Text
	}
	row := ui.Row(c).Gap(8).AlignItems(ui.Start)
	row.Children(func() {
		ui.Icon(c, icon).FontSize(iconInline).TextColor(col).MarginY(1)
		el := ui.Text(c, text).FontSize(plat.calloutSize).TextColor(txt).Grow(1).Wrap().LineHeight(lineText).Selectable()
		if raw && m.Text != "" {
			el.Tooltip(m.Text)
		}
	})
	if m.Text != "" {
		row.ContextMenu(func(menu *ui.Menu) {
			if menu.Item(i18n.T.MsgCopyRaw).Chosen() {
				copyText(c, m.Text)
			}
		})
	}
	return row
}

// errorMsg codes an error text the helper returned (an RPC error's
// message, ApplyResult.Error) for msgNote.
func errorMsg(text string, coded *rpc.Msg) rpc.Msg {
	if coded != nil && coded.Code != "" {
		return *coded
	}
	text = strings.TrimPrefix(text, i18n.T.ErrorPrefix)
	return msg.Parse(text)
}

// layerErrorLine returns the editor line a coded layer error points at,
// and the layer it names; 0 when it has none.
func layerErrorLine(m rpc.Msg) (layer string, line int) {
	if !strings.HasPrefix(m.Code, "layer-") || len(m.Args) < 2 {
		return "", 0
	}
	n := 0
	for _, r := range m.Args[1] {
		if r < '0' || r > '9' {
			return m.Args[0], 0
		}
		n = n*10 + int(r-'0')
	}
	return m.Args[0], n
}

// resultMessages is a compose/apply result's warnings as codes, without
// the ones another row already says (the impact row).
func resultMessages(r rpc.ComposeResult, impact *rpc.ReloadImpact) []rpc.Msg {
	ms := msg.Of(r)
	if impact == nil {
		return ms
	}
	return slices.DeleteFunc(slices.Clone(ms), func(m rpc.Msg) bool {
		return m.Code == "reload-disrupts" || m.Code == "restart-needed"
	})
}
