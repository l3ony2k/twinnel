package main

import (
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
)

// --- logs ---

var logLevels = []string{"debug", "info", "warning", "error"}

type logsView struct {
	level  int // index into logLevels; zero value means "info" via init
	inited bool
	paused bool
	frozen []mihomo.LogEntry
	list   ui.ListState
}

func (l *logsView) levelName() string {
	if !l.inited {
		l.level, l.inited = 1, true
	}
	return logLevels[l.level]
}

func (a *app) logsPage(c *ui.Context, barH float32) {
	t := c.Theme()
	T := i18n.T
	p := pal(t)
	a.logs.levelName()
	entries := a.mon.Logs()
	if a.logs.paused {
		entries = a.logs.frozen
	}
	ui.Column(c).Grow(1).Padding(barH+4, plat.pagePadX, 20).Gap(16).Children(func() {
		pageHeader(c, T.PageLogs, func() {
			small(c, T.LogLevel)
			if ui.Segmented(c.Key("level"), &a.logs.level, logLevels...).Changed() {
				a.mon.ClearLogs()
				a.mon.SetLogLevel(a.logs.levelName())
			}
			pause := T.LogPause
			if a.logs.paused {
				pause = T.LogResume
			}
			if ui.Button(c.Key("pause"), pause).Clicked() {
				a.logs.paused = !a.logs.paused
				a.logs.frozen = a.mon.Logs()
			}
			if ui.Button(c.Key("clear"), T.LogClear).Clicked() {
				a.mon.ClearLogs()
				a.logs.frozen = nil
			}
		})
		box := ui.Column(c).Grow(1).Radius(plat.groupRadius).Background(p.group).Border(1, p.groupLine).Clip()
		box.Children(func() {
			if len(entries) == 0 {
				ui.Column(c).Grow(1).Justify(ui.Center).Children(func() {
					emptyState(c, iconLogs, T.LogEmpty, T.LogEmptyHint)
				})
				return
			}
			ui.List(c, &a.logs.list, len(entries), func(i int) {
				e := entries[i]
				col := t.Text
				switch e.Type {
				case "warning":
					col = p.warnText
				case "error":
					col = p.badText
				case "debug":
					col = t.TextMuted
				}
				ui.Row(c).Gap(12).Padding(3, 12).AlignItems(ui.Start).Children(func() {
					ui.Text(c, e.Time.Format("15:04:05")).Font("monospace").FontSize(11).TextColor(t.TextMuted).Shrink(0)
					ui.Text(c, e.Type).Font("monospace").FontSize(11).TextColor(col).Width(56).Shrink(0)
					ui.Text(c, e.Payload).Font("monospace").FontSize(11).TextColor(col).Grow(1).Wrap().Selectable()
				})
			}).Grow(1).Background(ui.Transparent).PaddingY(6)
		})
	})
}
