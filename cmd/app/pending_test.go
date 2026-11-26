package main

import (
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/i18n"
)

func TestBusySpan(t *testing.T) {
	t0 := time.Unix(1000, 0)
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	ms := time.Millisecond
	cases := []struct {
		name string
		span busySpan
		now  time.Duration
		want bool
	}{
		{"none", busySpan{}, 0, false},
		{"running, before the delay", busySpan{start: t0}, 299 * ms, false},
		{"running, after the delay", busySpan{start: t0}, 300 * ms, true},
		{"running long", busySpan{start: t0}, time.Minute, true},
		{"quick answer never shows", busySpan{start: t0, end: at(200 * ms)}, 350 * ms, false},
		{"slow answer stays its minimum", busySpan{start: t0, end: at(400 * ms)}, 700 * ms, true},
		{"minimum over", busySpan{start: t0, end: at(400 * ms)}, 800 * ms, false},
		{"long run ends at once", busySpan{start: t0, end: at(3 * time.Second)}, 3*time.Second + ms, false},
	}
	for _, c := range cases {
		if got := c.span.visible(at(c.now)); got != c.want {
			t.Errorf("%s: visible = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPendingValue(t *testing.T) {
	a := &app{}
	a.pend.vals = map[string]*pendingEntry{}
	if got := pendingValue(a, pendTun, false); got {
		t.Fatal("no pending value: want the actual one")
	}
	// A call runs: the chosen value shows even though the snapshot agrees
	// with the old one, and also when it already agrees.
	e := &pendingEntry{val: true, inflight: 1}
	a.pend.vals[pendTun] = e
	if !pendingValue(a, pendTun, false) {
		t.Error("while the call runs: want the chosen value")
	}
	// The call ended: kept until the snapshot agrees ...
	e.inflight, e.until = 0, time.Now().Add(time.Minute)
	if !pendingValue(a, pendTun, false) {
		t.Error("before the snapshot agrees: want the chosen value")
	}
	if !pendingValue(a, pendTun, true) || a.pend.vals[pendTun] != nil {
		t.Error("the snapshot agrees: want it forgotten")
	}
	// ... or until it has waited long enough.
	a.pend.vals[pendTun] = &pendingEntry{val: true, until: time.Now().Add(-time.Second)}
	if pendingValue(a, pendTun, false) {
		t.Error("expired: want the actual value")
	}
	// A value of another type is dropped.
	a.pend.vals[pendMode] = &pendingEntry{val: 3, inflight: 1}
	if pendingValue(a, pendMode, "rule") != "rule" || a.pend.vals[pendMode] != nil {
		t.Error("wrong type: want the actual value")
	}
}

func TestOpResult(t *testing.T) {
	T := i18n.T
	quiet := []string{T.LayerReorder, i18n.F(T.LayerToggle, "x"), T.SubActivate + " main", T.Preview, T.KernelCheck}
	for _, l := range quiet {
		if _, q := opResult(l); !q {
			t.Errorf("%q: want no toast", l)
		}
	}
	for label, want := range map[string]string{
		T.Apply:                T.DoneApplied,
		T.HistoryRollback:      T.DoneRolledBack,
		T.SubRefresh + " main": "已更新订阅「main」",
		T.SubSave + " main":    "已保存订阅「main」",
		T.Remove + " main":     "已删除「main」",
		T.Save + " dns":        "已保存覆写「dns」",
		T.Remove:               i18n.F(T.OpDone, T.Remove),
		T.TakeoverAdopt:        i18n.F(T.OpDone, T.TakeoverAdopt),
	} {
		if got, q := opResult(label); q || got != want {
			t.Errorf("%q: got %q (quiet %v), want %q", label, got, q, want)
		}
	}
}
