package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/agent/agenttest"
)

// compose --preview prints the semantic diff against the running config
// (from the fake helper), summary first.
func TestComposePreviewDiff(t *testing.T) {
	ctrl := agenttest.NewController(t)
	h := agenttest.NewHelper(t, ctrl.Addr())
	var out bytes.Buffer
	old := stdout
	stdout = &out
	t.Cleanup(func() { stdout = old })
	if err := runComposeHelper([]string{"--preview", "--socket", h.Socket}); err != nil {
		t.Fatal(err)
	}
	txt := out.String()
	for _, want := range []string{"mihomo -t:", "summary: ", "example.org"} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in:\n%s", want, txt)
		}
	}
	for _, s := range agenttest.Secrets {
		if strings.Contains(txt, s) {
			t.Errorf("output leaks %q", s)
		}
	}
}
