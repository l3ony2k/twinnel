package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// --- logs ---

// The 日志 page has three sources (U29): mihomo's log stream, what
// Twinnel did (the helper's events), and the helper's own log. All search
// and filter by level.

var logLevels = []string{"debug", "info", "warning", "error"}

const (
	logsMihomo  = 0
	logsTwinnel = 1
	logsHelper  = 2
)

type logsView struct {
	level  int // index into logLevels; zero value means "info" via init
	inited bool
	paused bool
	frozen []mihomo.LogEntry
	list   ui.ListState

	source int    // logsMihomo, logsTwinnel or logsHelper
	query  string // search text, every source
	evList ui.ListState
	mark   uint64 // the event a toast pointed at (H7), highlighted

	// The Twinnel tab's own 暂停 / 清空: a frozen copy of the events, and the
	// last event 清空 hid (the helper's buffer is not touched).
	evPaused  bool
	evFrozen  []rpc.Event
	evCleared uint64

	width float32 // the page's width last frame (the bar's search follows it)

	findKey bool // Ctrl+F (⌘F) or F3 was pressed: the search takes the focus on the next frame

	svc serviceLog // mihomo's service output, shown before the stream (servicelog.go)

	// The helper tab: its log file, and its own 暂停 / list.
	helper   serviceLog
	hPaused  bool
	hFrozen  []mihomo.LogEntry
	helperLs ui.ListState

	// atEnd is source+1 of the list last sent to its newest line since
	// the page opened (0: none yet; pageChanged resets it): opening the
	// page, by any way (a launch's --open=page:logs too), or another tab
	// shows the newest lines first (followNewest).
	atEnd int
}

// followNewest makes the mihomo and 后台助手 lists, oldest line first,
// show the newest: at the end when the page or the tab opens, and kept
// there as lines come while the user is at the bottom (FollowEnd; one
// who scrolls up stays put until back at the bottom).
func (l *logsView) followNewest(list *ui.ListState) {
	list.FollowEnd = true
	if l.atEnd != l.source+1 {
		list.ScrollToEnd()
		l.atEnd = l.source + 1
	}
}

func (l *logsView) levelName() string {
	if !l.inited {
		l.level, l.inited = 1, true
	}
	return logLevels[l.level]
}

// showTwinnelLog opens 日志 → Twinnel at its newest event (a toast's 查看).
func (a *app) showTwinnelLog() {
	a.logs.source = logsTwinnel
	if n := len(a.events.evs); n > 0 {
		a.logs.mark = a.events.evs[n-1].Seq
	}
	a.openPage(pageLogs)
}

// mihomoLogs is 日志 → mihomo's list: the service output, then the stream.
func (a *app) mihomoLogs() []mihomo.LogEntry {
	return withServiceLog(a.logs.svc.entries, a.mon.Logs(), a.logs.levelName(), a.logs.svc.cleared)
}

// helperLogs is 日志 → 后台助手's list: the helper's log at the chosen
// level, not cleared.
func (a *app) helperLogs() []mihomo.LogEntry {
	return withServiceLog(a.logs.helper.entries, nil, a.logs.levelName(), a.logs.helper.cleared)
}

func (a *app) logsPage(c *ui.Context, barH float32) {
	a.logs.levelName()
	var entries []mihomo.LogEntry
	switch a.logs.source {
	case logsMihomo:
		a.fetchServiceLog(rpc.ServiceLogMihomo, &a.logs.svc)
		entries = a.mihomoLogs()
		if a.logs.paused {
			entries = a.logs.frozen
		}
	case logsHelper:
		a.fetchServiceLog(rpc.ServiceLogHelper, &a.logs.helper)
		entries = a.helperLogs()
		if a.logs.hPaused {
			entries = a.logs.hFrozen
		}
	case logsTwinnel:
		a.backfillEvents()
	}
	// Logs fill the pane, and so does their toolbar (window.go): long
	// lines want the width.
	col := ui.Column(c).Grow(1).Padding(barH+4, plat.pagePadX, 20)
	col.Children(func() {
		a.logsBody(c, entries)
	})
	if w := col.Bounds().W; w > 0 && w != a.logs.width { // the bar's search follows the width
		a.logs.width = w
		c.Invalidate()
	}
}

// levelMenu is the level pop-up of the 日志 toolbar: its value is the
// chosen level, the menu lists the four. It reports a change.
func (a *app) levelMenu(c *ui.Context) bool {
	T := i18n.T
	v := &a.logs
	changed := false
	names := T.LogLevelNames
	cur := logLevelName(v.levelName())
	pushMenu(c.Key("level"), cur, func(m *ui.Menu) {
		for i, n := range names {
			if m.Item(n).Checked(i == v.level).Chosen() && i != v.level {
				v.level = i
				changed = true
			}
		}
	}).Label(T.LogLevel + " " + cur).Tooltip(T.LogLevel)
	return changed
}

// barIcon is an icon-only toolbar button (the toolbar's capsule, as
// wide as it is tall), its name in the tooltip and for VoiceOver.
func barIcon(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	b := pushButton(c, "").Label(label).Tooltip(label)
	if inToolbar {
		b.Width(toolbarItemH).Padding(0)
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(iconInline)
	})
	return b
}

// logSearch is the bar's search: a search field with its magnifier on
// Windows (WinUI's AutoSuggestBox with the search glyph), the plain field
// elsewhere. The find keys put the focus in it from anywhere on the page:
// Ctrl+F (⌘F on macOS), and F3 on Windows, as Explorer and Settings do.
func (a *app) logSearch(c *ui.Context, width float32) ui.Element {
	T := i18n.T
	v := &a.logs
	var f ui.Element
	if plat.fluent {
		f = ui.SearchField(c.Key("search"), &v.query)
	} else {
		f = ui.TextInput(c.Key("search"), &v.query)
	}
	f.Label(T.LogSearch).Placeholder(T.LogSearch).Width(width)
	for _, k := range findKeys() {
		c.OnWindowShortcut(k.mods, k.key, func() { v.findKey = true; c.Invalidate() })
	}
	if v.findKey {
		v.findKey = false
		f.Focus()
	}
	return f
}

// shortcutKey is a key with its modifiers.
type shortcutKey struct {
	mods ui.Modifiers
	key  ui.Key
}

// findKeys put the focus in a page's search: Ctrl+F (⌘F), and F3 on
// Windows.
func findKeys() []shortcutKey {
	keys := []shortcutKey{{ui.Cmd, ui.KeyF}}
	if plat.fluent {
		keys = append(keys, shortcutKey{0, ui.KeyF3})
	}
	return keys
}

// matches reports whether text holds every word of the query, ignoring
// case.
func matches(text, query string) bool {
	text = strings.ToLower(text)
	for _, w := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

func (a *app) logsBody(c *ui.Context, entries []mihomo.LogEntry) {
	t := c.Theme()
	T := i18n.T
	p := pal(t)
	v := &a.logs
	ui.Column(c).Grow(1).Gap(16).Children(func() {
		pageHeader(c, T.PageLogs, func() {
			// What the bar holds, in the order it gives way: the source,
			// its ⓘ, the search, the level as a pop-up (its value names
			// it), then 暂停 and 清空 as icons. It fits a 900 pt window in
			// every language; the title never drops (R-kubai-1).
			selectorBar(c.Key("source"), &v.source, T.LogsSourceMihomo, T.LogsSourceTwinnel, T.LogsSourceHelper).Label(T.PageLogs)
			infoTipAbout(c, T.PageLogs, T.TipLogs, nil)
			searchW := float32(200)
			if v.width > 0 && v.width < 760 {
				searchW = 140
			}
			a.logSearch(c, searchW)
			if a.levelMenu(c) && v.source == logsMihomo {
				a.mon.ClearLogs()
				a.mon.SetLogLevel(v.levelName())
			}
			// A toggle: 暂停 and 继续 are one place, so the bar does not
			// shift as it flips; the state is also spoken (selected). Both
			// tabs have both buttons, each with its own state.
			paused := &v.paused
			switch v.source {
			case logsTwinnel:
				paused = &v.evPaused
			case logsHelper:
				paused = &v.hPaused
			}
			icon, pause := iconPause, T.LogPause
			if *paused {
				icon, pause = iconPlay, T.LogResume
			}
			b := barIcon(c.Key("pause"), icon, pause).Role(ui.RoleToggleButton).AccessSelected(*paused)
			if *paused {
				b.Tooltip(T.LogPaused)
			}
			if b.Clicked() {
				*paused = !*paused
				switch v.source {
				case logsTwinnel:
					v.evFrozen = append([]rpc.Event(nil), a.events.evs...)
				case logsHelper:
					v.hFrozen = a.helperLogs()
				default:
					v.frozen = a.mihomoLogs()
				}
			}
			if barIcon(c.Key("clear"), iconClear, T.LogClear).Clicked() {
				switch v.source {
				case logsTwinnel:
					if n := len(a.events.evs); n > 0 {
						v.evCleared = a.events.evs[n-1].Seq
					}
					v.evFrozen = nil
				case logsHelper:
					v.hFrozen = nil
					v.helper.cleared = time.Now()
				default:
					a.mon.ClearLogs()
					v.frozen = nil
					v.svc.cleared = time.Now()
				}
			}
		})
		box := ui.Column(c).Grow(1).Radius(plat.groupRadius).Background(p.group).Border(1, p.groupLine).Clip()
		box.Children(func() {
			if v.source == logsTwinnel {
				a.eventsList(c)
				return
			}
			if v.query != "" {
				var kept []mihomo.LogEntry
				for _, e := range entries {
					if matches(e.Payload, v.query) {
						kept = append(kept, e)
					}
				}
				if len(entries) > 0 && len(kept) == 0 {
					a.logsEmpty(c, T.LogNoMatch, "")
					return
				}
				entries = kept
			}
			list := &v.list
			if v.source == logsHelper {
				list = &v.helperLs
				if len(entries) == 0 && v.helper.failed && len(v.helper.entries) == 0 {
					a.logsEmpty(c, T.LogHelperUnavailable, "")
					return
				}
			}
			if len(entries) == 0 {
				a.logsEmpty(c, T.LogEmpty, T.LogEmptyHint)
				return
			}
			v.followNewest(list)
			ui.List(c, list, len(entries), func(i int) {
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
				line := e.Time.Format("15:04:05") + " " + e.Type + " " + e.Payload
				ui.Row(c).Gap(12).Padding(3, 12).AlignItems(ui.Start).Children(func() {
					ui.Text(c, e.Time.Format("15:04:05")).Font("monospace").FontSize(plat.smallSize).TextColor(t.TextMuted).Shrink(0)
					colLabel(c, ui.Span{Size: plat.smallSize}, logLevelName(e.Type), levelColW).TextColor(col)
					ui.Text(c, e.Payload).Font("monospace").FontSize(plat.smallSize).TextColor(col).Grow(1).Wrap().LineHeight(lineText).Selectable().
						ContextMenu(func(m *ui.Menu) {
							if m.Item(i18n.T.CopyLine).Chosen() {
								copyText(c, line)
							}
							m.Separator()
							m.EditItems()
						})
				}).Label(logLevelName(e.Type) + " " + e.Payload)
			}).Grow(1).Background(ui.Transparent).PaddingY(6)
		})
	})
}

func (a *app) logsEmpty(c *ui.Context, title, hint string) {
	ui.Column(c).Grow(1).Justify(ui.Center).Children(func() {
		emptyState(c, iconLogs, title, hint)
	})
}

// eventsList is 日志 → Twinnel: newest first, one row each: time · kind ·
// what happened; a click goes where it happened (a history entry, a page).
func (a *app) eventsList(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	p := pal(t)
	v := &a.logs
	type row struct {
		e rpc.Event
		r eventRow
	}
	var rows []row
	evs := a.events.evs
	if v.evPaused {
		evs = v.evFrozen
	}
	if v.evCleared != 0 {
		k := 0
		for k < len(evs) && evs[k].Seq <= v.evCleared {
			k++
		}
		evs = evs[k:]
	}
	for i := len(evs) - 1; i >= 0; i-- {
		r, ok := describeEvent(evs[i], a.eventName)
		if !ok {
			continue
		}
		// Levels: a failure is an error; everything else is info, so
		// 警告 and 错误 keep only the failures.
		if v.level >= 2 && !r.failed {
			continue
		}
		if v.query != "" && !matches(r.kind+" "+r.text, v.query) {
			continue
		}
		rows = append(rows, row{evs[i], r})
	}
	if len(rows) == 0 {
		if len(evs) > 0 {
			a.logsEmpty(c, T.LogNoMatch, "")
		} else {
			a.logsEmpty(c, T.EventsEmpty, "")
		}
		return
	}
	ui.List(c, &v.evList, len(rows), func(i int) {
		e, r := rows[i].e, rows[i].r
		col := t.Text
		if r.failed {
			col = p.badText
		}
		when := e.Time.Local().Format("01-02 15:04:05")
		el := ui.Row(c).Gap(12).Padding(5, 12).MarginX(6).Radius(plat.controlRadius).AlignItems(ui.Center).
			Focusable().Label(r.kind + " " + r.text)
		if e.Seq == v.mark {
			el.Background(p.accentSoft)
		} else if el.Hovered() && r.page != "" {
			el.Background(t.Text.Alpha(0.04))
		}
		el.ContextMenu(func(m *ui.Menu) {
			if m.Item(i18n.T.CopyLine).Chosen() {
				copyText(c, when+" "+r.kind+" "+r.text)
			}
			if r.raw != "" && m.Item(i18n.T.MsgCopyRaw).Chosen() {
				copyText(c, r.raw)
			}
		})
		el.Children(func() {
			num(ui.Text(c, when).FontSize(plat.smallSize).TextColor(t.TextMuted).Shrink(0))
			tag(c, r.kind)
			ui.Text(c, r.text).FontSize(plat.calloutSize).TextColor(col).Grow(1).Shrink(1).Wrap().LineHeight(lineText)
		})
		if el.Clicked() && r.page != "" {
			v.mark = e.Seq
			if r.hist != "" {
				a.showHistoryEntry(r.hist)
			} else {
				a.openPage(r.page)
			}
		}
	}).Grow(1).Background(ui.Transparent).PaddingY(6)
}

// subscriptionName names a subscription by ID for the events.
// eventName names a subscription or an override by ID for the events.
func (a *app) eventName(kind, id string) string {
	if kind == "layer" {
		for _, l := range a.hd.layers {
			if l.ID == id {
				return layerTitle(l)
			}
		}
		return id
	}
	return a.subscriptionName(id)
}

func (a *app) subscriptionName(id string) string {
	for _, s := range a.hd.subs {
		if s.ID == id && s.Name != "" {
			return s.Name
		}
	}
	return id
}

// logLevelName is a log entry's level in the UI language; unknown levels
// show as they come.
func logLevelName(level string) string {
	for i, l := range logLevels {
		if l == level && i < len(i18n.T.LogLevelNames) {
			return i18n.T.LogLevelNames[i]
		}
	}
	return level
}
