package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/status"
)

// bigConfigText is a synthetic ~10k-rule config text.
func bigConfigText(extra string) string {
	var b strings.Builder
	b.WriteString("mixed-port: 7890\nmode: rule\nrules:\n")
	b.WriteString(extra)
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&b, "  - DOMAIN-SUFFIX,site%d.example,PROXY\n", i)
	}
	return b.String()
}

// BenchmarkPreviewDiff10k is the preview's diff of two 10k-rule configs
// differing in a few lines, as the apply card makes it (main thread).
func BenchmarkPreviewDiff10k(b *testing.B) {
	old := bigConfigText("")
	next := bigConfigText("  - DOMAIN-SUFFIX,lan.example,DIRECT\n  - IP-CIDR,192.0.2.0/24,DIRECT,no-resolve\n")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := newDiff(old, next)
		if add, del := d.Stats(); add != 2 || del != 0 {
			b.Fatal(fmt.Sprint(add, del))
		}
	}
}

// bigLayerText is a "+rules:" override with 10k rules.
func bigLayerText() string {
	var b strings.Builder
	b.WriteString("+rules:\n")
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&b, "  - DOMAIN-SUFFIX,site%d.example,PROXY\n", i)
	}
	return b.String()
}

// BenchmarkLintLayer10k is the editor's lint of a 10k-rule override
// (main thread, once per edit).
func BenchmarkLintLayer10k(b *testing.B) {
	text := bigLayerText()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		status.LintOverride(text)
	}
}

// BenchmarkSummarizeLayer10k is a 10k-rule override's row summary (main
// thread, once per content).
func BenchmarkSummarizeLayer10k(b *testing.B) {
	text := bigLayerText()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		summarizeLayer(text)
	}
}
