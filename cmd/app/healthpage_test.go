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

// 出口 IP 实测 where the test can't run (platform): the row says so in one
// line, offers no 检测出口 and no switch, and only while 定期检测 is on a
// button to turn it off. The exit tile and the fix buttons don't offer the
// test either.
func TestEgressRowUnsupported(t *testing.T) {
	defer i18n.Set("zh")
	i18n.Set("zh")
	cr := health.CheckReport{ID: health.LeakEgress{}.ID(), Title: "出口 IP 实测",
		Result: health.Result{Status: health.StatusSkip, Summary: "在 Windows 上做不了这项检查", Unsupported: true}}
	at := time.Date(2026, 11, 27, 11, 5, 0, 0, time.Local)
	s := status.Snapshot{Time: at, WritesAllowed: true}
	s.Helper.State = status.HelperConnected
	s.Health.Report = &health.Report{Started: at, Checks: []health.CheckReport{cr}}
	for _, periodic := range []bool{true, false} {
		a := &app{healthOpen: map[string]bool{}, egressProbe: func(context.Context) *health.CheckReport { return nil }}
		a.hd.settings = &rpc.Settings{HealthEgress: periodic}
		if a.canTestEgress(s) {
			t.Fatal("test offered where it can't run")
		}
		if eg, _ := a.egressNow(s); eg != nil {
			t.Fatalf("exit tile shows an unsupported result: %+v", eg)
		}
		tr := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Padding(20, 20).Children(func() { a.healthCheckRow(c.Key("r"), cr, s, false) })
		}, 800, 500)
		if !tr.HasText(cr.Summary) {
			t.Errorf("periodic=%v: no reason in %v", periodic, tr.Texts())
		}
		for _, no := range []string{i18n.T.FixTestEgress, i18n.T.EgressRetest, i18n.T.EgressPeriodic, i18n.T.EgressUntested} {
			for _, got := range tr.Texts() {
				if got == no {
					t.Errorf("periodic=%v: %q offered in %v", periodic, no, tr.Texts())
				}
			}
		}
		if tr.HasText(i18n.T.EgressPeriodicOff) != periodic {
			t.Errorf("periodic=%v: turn-off button shown=%v", periodic, !periodic)
		}
	}
}
