package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// One timer for every busy mark (busy.go): track and next agree with
// visible.
func TestBusySpanTrack(t *testing.T) {
	ms := time.Millisecond
	t0 := time.Unix(1000, 0)
	var b busySpan
	b.track(true, t0)
	if b.visible(t0.Add(299 * ms)) {
		t.Fatal("shown before busyDelay")
	}
	if got := b.next(t0); !got.Equal(t0.Add(busyDelay)) {
		t.Fatalf("next = %v, want the delay's end", got)
	}
	b.track(false, t0.Add(400*ms)) // ended after it showed: keeps busyMin
	if !b.visible(t0.Add(700 * ms)) {
		t.Fatal("hidden before busyMin")
	}
	if got := b.next(t0.Add(700 * ms)); !got.Equal(t0.Add(busyDelay + busyMin)) {
		t.Fatalf("next = %v, want the minimum's end", got)
	}
	// Busy again while the mark still shows: one run, it stays.
	b.track(true, t0.Add(750*ms))
	if !b.visible(t0.Add(2 * time.Second)) {
		t.Fatal("a resumed run lost its mark")
	}
	// A quick answer never shows.
	var q busySpan
	q.track(true, t0)
	q.track(false, t0.Add(100*ms))
	if q.visible(t0.Add(350*ms)) || !q.next(t0.Add(350*ms)).IsZero() {
		t.Fatal("a quick answer showed a mark")
	}
	// A new run starts once the last one is over.
	q.track(true, t0.Add(time.Second))
	if !q.start.Equal(t0.Add(time.Second)) || !q.end.IsZero() {
		t.Fatalf("new run = %+v", q)
	}
}

func TestToastKind(t *testing.T) {
	p := i18n.T.ErrorPrefix
	cases := []struct {
		in         ui.Toast
		kind, text string
	}{
		{ui.Toast{Title: "已保存", Type: toastSuccess}, toastSuccess, "已保存"},
		{ui.Toast{Title: p + "boom"}, toastError, "boom"},
		{ui.Toast{Title: "已复制"}, "", "已复制"},
		{ui.Toast{Title: p + "x", Type: toastSuccess}, toastSuccess, p + "x"}, // a typed toast keeps its text
	}
	for _, c := range cases {
		k, txt := toastKind(c.in)
		if k != c.kind || txt != c.text {
			t.Errorf("toastKind(%q, %q) = %q, %q; want %q, %q", c.in.Title, c.in.Type, k, txt, c.kind, c.text)
		}
	}
}

func TestDiffLines(t *testing.T) {
	got := diffLines("--- a\n+++ b\n@@ -1,2 +1,2 @@\n rules:\n-  - A\n+  - B\n")
	want := []diffLine{{' ', "rules:"}, {'-', "  - A"}, {'+', "  - B"}}
	if len(got) != len(want) {
		t.Fatalf("got %d lines %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// The page column is left-anchored and wide enough for tables (unslop
// R-2); tooltips open at AppKit's half second (kubai R-16).
func TestPlatformSurfaceTokens(t *testing.T) {
	if p := platformFor("darwin"); p.contentMax != 1080 {
		t.Errorf("darwin contentMax = %v, want 1080", p.contentMax)
	}
	if d := systemTheme(true, ui.Preferences{}).TooltipDelay; d != 500*time.Millisecond {
		t.Errorf("TooltipDelay = %v", d)
	}
}
