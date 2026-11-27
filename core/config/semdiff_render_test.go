package config

import (
	"fmt"
	"strings"
	"testing"
)

// A subscription with many dead rules and a change that touches only one
// setting: the compact rendering folds the dead rules both sides share
// into a count and a few examples, and puts the setting change first.
func TestRenderFoldsSharedDeadRules(t *testing.T) {
	var rules strings.Builder
	rules.WriteString("  - DOMAIN-SUFFIX,example.com,DIRECT\n")
	for i := range 30 {
		fmt.Fprintf(&rules, "  - DOMAIN,host%d.example.com,PROXY\n", i) // covered by the suffix rule
	}
	rules.WriteString("  - MATCH,PROXY\n")
	cfg := func(port int, extra string) []byte {
		return []byte(fmt.Sprintf("mixed-port: %d\nproxies: []\nproxy-groups:\n  - {name: PROXY, type: select, proxies: [DIRECT]}\nrules:\n%s%s", port, extra, rules.String()))
	}
	d, err := SemanticDiff(cfg(7890, ""), cfg(7899, "  - DOMAIN,only-b.example.net,DIRECT\n  - DOMAIN,only-b.example.net,PROXY\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Rules.DeadA) != 30 || len(d.Rules.DeadB) != 31 {
		t.Fatalf("dead A=%d B=%d", len(d.Rules.DeadA), len(d.Rules.DeadB))
	}

	txt := d.Render(TextOptions{More: "--verbose lists them all"})
	if !strings.HasPrefix(txt, "summary: 1 setting change; ") {
		t.Errorf("headline:\n%s", txt)
	}
	if i, j := strings.Index(txt, "mixed-port: 7890 → 7899"), strings.Index(txt, "rules:"); i < 0 || i > j {
		t.Errorf("the setting change is not before the rules:\n%s", txt)
	}
	for _, want := range []string{
		"shadowed or redundant in both A and B: 30 (unchanged), the first 5:",
		"(--verbose lists them all)",
		"in B only: #", "only-b.example.net,PROXY",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in:\n%s", want, txt)
		}
	}
	if n := strings.Count(txt, "host"); n != 5 {
		t.Errorf("%d folded examples shown, want 5:\n%s", n, txt)
	}
	if strings.Contains(txt, "in A only") {
		t.Errorf("A has no dead rule of its own:\n%s", txt)
	}

	full := d.Render(TextOptions{Verbose: true})
	if n := strings.Count(full, "host"); n != 60 {
		t.Errorf("verbose lists %d dead rules, want both sides in full (60):\n%s", n, full)
	}
	if d.Text(0) != full {
		t.Error("Text is not the verbose rendering")
	}
}

func TestSplitDead(t *testing.T) {
	a := []DeadRule{{Rule: "x"}, {Rule: "y"}, {Rule: "y"}, {Rule: "z", Redundant: true}}
	b := []DeadRule{{Rule: "y"}, {Rule: "z"}, {Rule: "x"}, {Rule: "w"}}
	common, onlyA, onlyB := splitDead(a, b)
	got := fmt.Sprint(len(common), len(onlyA), len(onlyB))
	if got != "2 2 2" || onlyA[0].Rule != "y" || onlyA[1].Rule != "z" || onlyB[0].Rule != "z" || onlyB[1].Rule != "w" {
		t.Fatalf("common %v onlyA %v onlyB %v", common, onlyA, onlyB)
	}
}
