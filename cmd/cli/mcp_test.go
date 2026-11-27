package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/l3ony2k/duet/core/agent/agenttest"
	"github.com/l3ony2k/duet/core/rpc"
)

// TestMain lets a test run this binary as `duet` (DUET_TEST_RUN_MAIN=1):
// the end-to-end MCP test talks to it over real stdin/stdout.
func TestMain(m *testing.M) {
	if os.Getenv("DUET_TEST_RUN_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestMCPStdioEndToEnd(t *testing.T) {
	ctrl := agenttest.NewController(t)
	h := agenttest.NewHelper(t, ctrl.Addr())
	h.Mu.Lock()
	h.Agent = rpc.AgentSettings{MCP: true, Operate: true}
	h.Mu.Unlock()
	ts, err := filepath.Abs("../../core/agent/testdata/tailscale-status.json")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "mcp")
	cmd.Env = append(os.Environ(),
		"DUET_TEST_RUN_MAIN=1",
		"DUET_SOCKET="+h.Socket,
		"DUET_CONTROLLER=", // credentials come from the (fake) helper
		"DUET_TAILSCALE_STATUS_FILE="+ts,
		"DUET_READ_ONLY=0",
		"HOME="+t.TempDir(),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cl := mcp.NewClient(&mcp.Implementation{Name: "e2e-agent", Version: "0.1"}, nil)
	cs, err := cl.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	for _, want := range []string{"status", "set_mode", "apply", "admin_only"} {
		if !names[want] {
			t.Errorf("tool %s missing", want)
		}
	}
	var outputs []string
	callTool := func(name string, args map[string]any) (string, bool) {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		text := res.Content[0].(*mcp.TextContent).Text
		outputs = append(outputs, text)
		return text, res.IsError
	}
	text, isErr := callTool("status", map[string]any{})
	if isErr {
		t.Fatalf("status: %s", text)
	}
	var st map[string]any
	json.Unmarshal([]byte(text), &st)
	if st["mihomo"].(map[string]any)["mode"] != "rule" {
		t.Fatalf("status = %s", text)
	}
	if text, isErr = callTool("set_mode", map[string]any{"mode": "direct"}); isErr {
		t.Fatalf("set_mode: %s", text)
	}
	if text, isErr = callTool("layer_put", map[string]any{"id": "x", "content": "a: 1"}); !isErr || !strings.Contains(text, "configure") {
		t.Fatalf("layer_put with configure off = %v %s", isErr, text)
	}
	ctrl.Mu.Lock()
	mode := ctrl.Mode
	ctrl.Mu.Unlock()
	if mode != "direct" {
		t.Fatalf("controller mode = %s", mode)
	}
	h.Mu.Lock()
	recs := append([]rpc.AgentAction(nil), h.Records...)
	h.Mu.Unlock()
	if len(recs) != 1 || recs[0].Tool != "set_mode" || recs[0].Client != "e2e-agent 0.1" {
		t.Fatalf("recorded %+v", recs)
	}
	for _, out := range outputs {
		for _, s := range agenttest.Secrets {
			if strings.Contains(out, s) {
				t.Errorf("output leaks %q: %s", s, out)
			}
		}
	}
}
