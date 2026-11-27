package agent

import (
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
)

func TestHealthFixTiers(t *testing.T) {
	e := newEnv(t, nil)
	e.setAgent(rpc.AgentSettings{MCP: true, Operate: true, Configure: true})
	m := e.ok("health_fixes", nil)
	if !strings.Contains(e.output[len(e.output)-1], "exit-node-off") {
		t.Fatalf("health_fixes lists no exit-node fix: %v", m)
	}
	// The exit node is an operate-tier fix: the configure tool sends it back.
	e.fails("health_fix_config", map[string]any{"check": "dns.tailnet"}, "use health_fix")
	r := e.ok("health_fix", map[string]any{"check": "dns.tailnet"})
	if r["action"] != "exit-node-off" {
		t.Fatalf("health_fix: %v", r)
	}
	// The undo says, before it is used, that it goes back to the failing state.
	if u, ok := r["undo"]; ok {
		if w, _ := r["undo_warning"].(string); !strings.Contains(w, "before the fix") {
			t.Fatalf("undo without its warning: %v", r)
		}
		r = e.ok("health_fix", map[string]any{"undo": u})
		if w, _ := r["warning"].(string); r["undone"] != "exit-node-off" || w == "" {
			t.Fatalf("undo: %v", r)
		}
	} else {
		t.Fatalf("exit-node-off without an undo: %v", r)
	}
	// Operate off: the fix is refused by the tier gate.
	e.setAgent(rpc.AgentSettings{MCP: true})
	e.fails("health_fix", map[string]any{"check": "dns.tailnet"}, "")
}
