package main

import (
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/i18n"
)

func TestPlanNote(t *testing.T) {
	T := i18n.T
	a := &app{}
	if got := a.planNote(); got != "" {
		t.Fatalf("idle: %q", got)
	}
	a.hd.planTick = true // no redraw timer in a test
	a.hd.busy, a.hd.spanOp = T.TakeoverPlan, T.TakeoverPlan
	a.hd.span = busySpan{start: time.Now()}
	if got := a.planNote(); got != T.TkPlanRunning {
		t.Fatalf("just started: %q", got)
	}
	a.hd.span.start = time.Now().Add(-5 * time.Second)
	if got := a.planNote(); !strings.HasPrefix(got, T.TkPlanRunning) || !strings.Contains(got, "5") {
		t.Fatalf("after 5 s: %q", got)
	}
	a.hd.busy = T.TakeoverOpen // another operation: the plain note
	if got := a.planNote(); got != "" {
		t.Fatalf("other op: %q", got)
	}
}
