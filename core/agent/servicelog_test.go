package agent

import (
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/agent/agenttest"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// logs source=mihomo-output reads the service output through the helper,
// parsed and redacted.
func TestLogsMihomoOutput(t *testing.T) {
	e := newEnv(t, nil)
	e.setAgent(rpc.AgentSettings{MCP: true})
	out, isErr := e.call("logs", map[string]any{"source": "mihomo-output", "level": "warning"})
	if isErr || !strings.Contains(out, "Parse config error") || !strings.Contains(out, `"error"`) {
		t.Fatalf("%v %s", isErr, out)
	}
	if strings.Contains(out, agenttest.SubToken) {
		t.Fatalf("token in output: %s", out)
	}
}

// logs source=helper reads the helper's own log the same way.
func TestLogsHelper(t *testing.T) {
	e := newEnv(t, nil)
	e.setAgent(rpc.AgentSettings{MCP: true})
	out, isErr := e.call("logs", map[string]any{"source": "helper", "level": "warning"})
	if isErr || !strings.Contains(out, "subscription fetch") || strings.Contains(out, "compose.apply") {
		t.Fatalf("%v %s", isErr, out)
	}
	if strings.Contains(out, agenttest.SubToken) {
		t.Fatalf("token in output: %s", out)
	}
}
