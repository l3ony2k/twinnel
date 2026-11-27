package agent

import (
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
)

// TestFriendlyArgErrors: a wrong call says what is wrong in the tool's
// terms, never the schema validator's text.
func TestFriendlyArgErrors(t *testing.T) {
	e := newEnv(t, nil)
	e.setAgent(rpc.AgentSettings{MCP: true, Operate: true, Configure: true})
	cases := []struct {
		tool string
		args map[string]any
		want []string
	}{
		{"admin_only", nil, []string{"admin_only: missing action; valid actions: ", "helper_uninstall", "kernel_install"}},
		{"admin_only", map[string]any{"action": "reboot"}, []string{`unknown action "reboot"; valid actions: `}},
		{"admin_only", map[string]any{"action": 3}, []string{"action must be a string; valid actions"}},
		{"set_mode", nil, []string{"set_mode: missing mode (rule, global or direct)"}},
		{"set_mode", map[string]any{"mode": "rule", "force": true}, []string{`unknown argument "force"; valid arguments: mode`}},
		{"status", map[string]any{"verbose": true}, []string{`unknown argument "verbose"; this tool takes no arguments`}},
		{"logs", map[string]any{"lines": 2.5}, []string{"lines must be a whole number"}},
		{"layer_put", map[string]any{"id": "x"}, []string{"missing content (the YAML patch)"}},
		{"layer_put", map[string]any{"id": "x", "content": "a: 1", "enabled": "yes"}, []string{"enabled must be true or false"}},
	}
	for _, c := range cases {
		text := e.fails(c.tool, c.args, c.want[0])
		for _, w := range c.want[1:] {
			if !strings.Contains(text, w) {
				t.Errorf("%s(%v) = %q, want %q", c.tool, c.args, text, w)
			}
		}
		for _, bad := range []string{"validating", "root:", "properties: ["} {
			if strings.Contains(text, bad) {
				t.Errorf("%s(%v) shows schema text: %q", c.tool, c.args, text)
			}
		}
	}
	// A correct call still works, with the omitted optional arguments.
	e.ok("set_mode", map[string]any{"mode": "global"})
}
