package main

import (
	"fmt"
	"testing"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
)

// The overview and the health page word one health report the same way.
func TestHealthHeadlineMatchesOverview(t *testing.T) {
	cnt := map[health.Status]int{health.StatusOK: 7, health.StatusFail: 2}
	page := healthHeadline("失败", cnt)
	s := status.Snapshot{Level: status.LevelError, Reasons: []status.Reason{{Level: status.LevelError, Text: page, Health: true}}}
	head, rest := levelHeadline(s)
	if head != page || len(rest) != 0 || page != "健康检查：2 项失败" {
		t.Fatalf("overview %q %v, page %q", head, rest, page)
	}
	s.Reasons = append(s.Reasons, status.Reason{Level: status.LevelError, Text: "x"})
	if head, rest = levelHeadline(s); head != i18n.T.StatusError || len(rest) != 2 {
		t.Fatalf("mixed causes: %q %v", head, rest)
	}
	if got := healthHeadline("跳过", map[health.Status]int{health.StatusSkip: 3}); got != "跳过" {
		t.Fatal(got)
	}
}

// One health number everywhere (U5): the sidebar badge, the overview's
// reason and the health page's headline say the same count for one
// snapshot, here 3 failures and 2 warnings.
func TestHealthBadgeEqualsHeadline(t *testing.T) {
	rep := &health.Report{}
	for _, st := range []health.Status{health.StatusFail, health.StatusFail, health.StatusFail, health.StatusWarn, health.StatusWarn, health.StatusOK} {
		rep.Checks = append(rep.Checks, health.CheckReport{Result: health.Result{Status: st}})
	}
	s := status.Snapshot{Loaded: true, Health: status.Health{Report: rep}}
	status.Evaluate(&s)
	n, _ := s.HealthBadge()
	page := healthHeadline("", rep.Counts())
	if n != 3 || page != fmt.Sprintf(i18n.T.ReasonHealthFail, n) {
		t.Fatalf("badge %d, page %q", n, page)
	}
	sections := navSections(0, n, toneAlert)
	if got := sections[0].items[3].count; got != 3 {
		t.Errorf("health row count %d", got)
	}
}
