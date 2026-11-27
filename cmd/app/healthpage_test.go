package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

func TestHealthCountsText(t *testing.T) {
	got := healthCountsText(map[health.Status]int{health.StatusOK: 8, health.StatusWarn: 3})
	if got != "通过 8 · 警告 3" {
		t.Errorf("got %q", got)
	}
	if got := healthCountsText(map[health.Status]int{}); got != "通过 0" {
		t.Errorf("got %q", got)
	}
}

func TestSplitEvidence(t *testing.T) {
	for _, tc := range []struct{ in, label, value string }{
		{"系统   host-a 10.0.0.1", "系统", "host-a 10.0.0.1"},
		{"mihomo host-b", "", "mihomo host-b"},
		{"mihomo  host-b", "mihomo", "host-b"},
		{"  indented  text", "", "  indented  text"},
		{"key: a  b", "", "key: a  b"},
		{"label   ", "", "label   "},
	} {
		l, v := splitEvidence(tc.in)
		if l != tc.label || v != tc.value {
			t.Errorf("splitEvidence(%q) = %q, %q", tc.in, l, v)
		}
	}
}

func TestSplitFirstWord(t *testing.T) {
	if l, v := splitFirstWord("mihomo a → b"); l != "mihomo" || v != "a → b" {
		t.Errorf("got %q %q", l, v)
	}
	if l, _ := splitFirstWord("路由表：a → b"); l != "" {
		t.Errorf("colon label split: %q", l)
	}
}

// 出口 IP 实测: off reads as a setting (no time, the test offered); an
// on-demand result carries its time and offers 重新检测 and 定期检测.
func TestEgressRow(t *testing.T) {
	defer i18n.Set("zh")
	i18n.Set("zh")
	off := health.CheckReport{ID: health.LeakEgress{}.ID(), Title: "出口 IP 实测",
		Result: health.Result{Status: health.StatusSkip, Summary: "未开启", Fix: &health.FixHint{Text: "x", AutoFix: health.FixEgressProbe}}}
	if !egressOff(off) {
		t.Fatal("the off row is not off")
	}
	at := time.Date(2026, 11, 27, 11, 5, 0, 0, time.Local)
	got := off
	got.Result = health.Result{Status: health.StatusOK, Summary: "不同", Evidence: []string{"经 mihomo 出口：203.0.x.x"}}
	got.At = at
	if egressOff(got) {
		t.Fatal("a result is off")
	}
	a := &app{healthOpen: map[string]bool{got.ID: true}, egressProbe: func(context.Context) *health.CheckReport { return nil }}
	a.hd.settings = &rpc.Settings{}
	s := status.Snapshot{Time: at.Add(time.Hour)}
	s.Helper.State = status.HelperConnected
	tr := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(20, 20).Children(func() { a.healthCheckRow(c.Key("r"), got, s, false) })
	}, 800, 500)
	for _, want := range []string{fmt.Sprintf(i18n.T.EgressTestedAt, "11:05"), i18n.T.EgressRetest, i18n.T.EgressPeriodic} {
		if !tr.HasText(want) {
			t.Errorf("no %q in %v", want, tr.Texts())
		}
	}
}
