package main

import (
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
