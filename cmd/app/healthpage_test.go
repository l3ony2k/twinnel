package main

import (
	"testing"

	"github.com/l3ony2k/duet/core/health"
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
