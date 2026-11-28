package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/status"
)

// synthLines are n synthetic log lines, oldest first, as mihomo's list
// holds them.
func synthLines(from, n int) []mihomo.LogEntry {
	t0 := time.Date(2026, 11, 28, 5, 0, 0, 0, time.Local)
	out := make([]mihomo.LogEntry, n)
	for i := range out {
		k := from + i
		out[i] = mihomo.LogEntry{Type: "info", Payload: fmt.Sprintf("synthetic line %03d", k), Time: t0.Add(time.Duration(k) * time.Second)}
	}
	return out
}

// Windows QA round 4: 日志 → mihomo opened by --open=page:logs showed
// lines hours old, not the newest. The mihomo and 后台助手 lists start at
// their newest line whenever the page or the tab opens, follow new lines
// while the user is at the bottom, and stay put while they read further up.
func TestLogListsFollowNewest(t *testing.T) {
	defer i18n.Set("zh")
	i18n.Set("zh")
	a := &app{mon: status.NewMonitor(status.Options{}), page: pageLogs}
	entries := synthLines(0, 300)
	tr := ui.NewTester(func(c *ui.Context) { a.logsBody(c, entries) }, 900, 500)
	shows := func(n int) bool { return tr.HasText(fmt.Sprintf("synthetic line %03d", n)) }

	// A launch straight onto the page (no pageChanged): the newest line.
	if !shows(299) || shows(0) {
		t.Fatalf("opened at the oldest lines: %v", tr.Texts())
	}
	// New lines while at the bottom: followed.
	entries = append(entries, synthLines(300, 5)...)
	tr.Frame()
	if !shows(304) {
		t.Fatalf("did not follow new lines: %v", tr.Texts())
	}

	// The user scrolls up to read: new lines do not pull the list down.
	for range 40 {
		tr.Scroll(450, 300, 0, -400)
	}
	if !shows(0) {
		t.Fatalf("scrolling up did not reach the top: %v", tr.Texts())
	}
	entries = append(entries, synthLines(305, 5)...)
	tr.Frame()
	if !shows(0) || shows(309) {
		t.Fatalf("new lines moved a list the user scrolled up: %v", tr.Texts())
	}

	// Another page and back: the newest lines again.
	a.showPage(pageOverview)
	a.showPage(pageLogs)
	tr.Frame()
	if !shows(309) || shows(0) {
		t.Fatalf("reopened at the old place: %v", tr.Texts())
	}

	// The 后台助手 tab opens at its newest line too.
	a.logs.source = logsHelper
	tr.Frame()
	if !shows(309) || shows(0) {
		t.Fatalf("helper tab: %v", tr.Texts())
	}
}
