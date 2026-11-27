package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"tailscale.com/ipn"

	"github.com/l3ony2k/twinnel/core/agent/agenttest"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// tsFake is the synthetic status file with recorded (not applied) writes.
type tsFake struct {
	tailscale.API
	mu    sync.Mutex
	edits []*ipn.MaskedPrefs
}

func (f *tsFake) EditPrefs(_ context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits = append(f.edits, mp)
	return &ipn.Prefs{}, nil
}

type env struct {
	t      *testing.T
	h      *agenttest.Helper
	ctrl   *agenttest.Controller
	ts     *tsFake
	srv    *Server
	cs     *mcp.ClientSession
	mu     sync.Mutex
	output []string // every text the server returned
}

func newEnv(t *testing.T, mod func(*Deps)) *env {
	t.Helper()
	ctrl := agenttest.NewController(t)
	h := agenttest.NewHelper(t, ctrl.Addr())
	ts := &tsFake{API: tailscale.FileAPI("testdata/tailscale-status.json")}
	d := Deps{
		Dial: func(ctx context.Context) (Caller, func(), error) {
			c, err := h.Dial(ctx)
			if err != nil {
				return nil, nil, err
			}
			return c, func() { c.Close() }, nil
		},
		Tailscale: ts,
		Version:   "0.9.0-test",
		LogWindow: 300 * time.Millisecond,
		Doctor: func(context.Context) (json.RawMessage, error) {
			return json.RawMessage(`{"overall":"ok","note":"controller secret ` + agenttest.ControllerSecret + `"}`), nil
		},
	}
	if mod != nil {
		mod(&d)
	}
	srv := New(d)
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := srv.MCP.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	cl := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1.0"}, nil)
	cs, err := cl.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close(); ss.Wait() })
	return &env{t: t, h: h, ctrl: ctrl, ts: ts, srv: srv, cs: cs}
}

// call runs a tool and returns its text and whether it is an error.
func (e *env) call(name string, args map[string]any) (string, bool) {
	e.t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := e.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		e.t.Fatalf("%s: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	e.mu.Lock()
	e.output = append(e.output, name+": "+b.String())
	e.mu.Unlock()
	return b.String(), res.IsError
}

func (e *env) ok(name string, args map[string]any) map[string]any {
	e.t.Helper()
	text, isErr := e.call(name, args)
	if isErr {
		e.t.Fatalf("%s failed: %s", name, text)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		e.t.Fatalf("%s: not a JSON object: %v\n%s", name, err, text)
	}
	return m
}

func (e *env) fails(name string, args map[string]any, want string) string {
	e.t.Helper()
	text, isErr := e.call(name, args)
	if !isErr {
		e.t.Fatalf("%s succeeded, want an error containing %q: %s", name, want, text)
	}
	if !strings.Contains(text, want) {
		e.t.Fatalf("%s error = %q, want it to contain %q", name, text, want)
	}
	return text
}

func (e *env) setAgent(a rpc.AgentSettings) {
	e.h.Mu.Lock()
	e.h.Agent = a
	e.h.Mu.Unlock()
}

// noSecrets fails when any output so far holds a fixture secret.
func (e *env) noSecrets() {
	e.t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, out := range e.output {
		for _, s := range agenttest.Secrets {
			if strings.Contains(out, s) {
				e.t.Errorf("output leaks %q:\n%s", s, out)
			}
		}
	}
}

func TestEveryToolRedacts(t *testing.T) {
	e := newEnv(t, nil)
	e.setAgent(rpc.AgentSettings{MCP: true, Operate: true, Configure: true})
	calls := map[string]map[string]any{
		"status": nil, "version": {"check_updates": false}, "health": {"run": true}, "proxies": nil,
		"connections": nil, "traffic": nil, "logs": nil, "subscriptions": nil, "layers": {"id": "nodes"},
		"history": {"diff": "h-1"}, "preview": nil, "takeover_plans": nil, "tailnet_peers": nil, "doctor": nil,
		"agent_activity": nil, "admin_only": {"action": "kernel_install"},
		"set_mode": {"mode": "global"}, "select_node": {"group": "Proxy", "node": "US 1"},
		"delay_test": {"group": "Proxy"}, "health_egress_run": nil, "set_tun": {"enabled": false}, "tailscale_up": nil, "tailscale_down": nil,
		"set_exit_node": {"peer": "exit-jp"}, "clear_exit_node": nil, "refresh_subscription": nil, "flush_fakeip": nil,
		"geo_update": {"proxy": "direct"}, "dashboard_install": nil,
		"layer_put":    {"id": "extra", "content": "proxies:\n  - {name: X, type: ss, password: " + agenttest.NodePassword + "}\n"},
		"layer_remove": {"id": "nodes"}, "layer_enable": {"id": "extra", "enabled": false},
		"layer_order": {"ids": []string{"extra"}}, "sub_put": {"id": "main", "url": agenttest.SubURL},
		"sub_activate": {"id": "spare"}, "sub_remove": {"id": "spare"}, "apply": nil,
		"history_rollback": {"id": "h-1"}, "settings_update": {"history_keep": 40},
		"health_fixes": nil, "health_fix": {"check": "dns.tailnet"}, "health_fix_config": {"check": "dns.tailnet"},
		"config_import": {"path": importFile(t, "subs.txt", "Main "+agenttest.SubURL+"\n"), "confirm": true},
	}
	for _, ti := range e.srv.Tools() {
		args, ok := calls[ti.Name]
		if !ok {
			t.Errorf("tool %s is not exercised by this test", ti.Name)
			continue
		}
		e.call(ti.Name, args)
	}
	// Errors carry secrets too: a failed sub_put quotes its URL.
	e.call("sub_put", map[string]any{"id": "x", "url": "https://bad.example.com/" + agenttest.SubToken})
	e.call("logs", map[string]any{"source": "mihomo"})
	for _, uri := range []string{resStatus, resHealth, resConfig} {
		r, err := e.cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("%s: %v", uri, err)
		}
		e.output = append(e.output, uri+": "+r.Contents[0].Text)
	}
	e.noSecrets()
	// And the activity log never got one.
	e.h.Mu.Lock()
	defer e.h.Mu.Unlock()
	b, _ := json.Marshal(e.h.Records)
	for _, s := range agenttest.Secrets {
		if strings.Contains(string(b), s) {
			t.Errorf("agent.record got %q: %s", s, b)
		}
	}
}

func TestMCPOffRefusesEverything(t *testing.T) {
	e := newEnv(t, nil)
	e.setAgent(rpc.AgentSettings{MCP: false, Operate: true, Configure: true})
	for name, args := range map[string]map[string]any{
		"status": nil, "proxies": nil, "set_mode": {"mode": "rule"}, "layer_put": {"id": "x", "content": "a: 1"},
		"doctor": nil, "tailnet_peers": nil,
	} {
		e.fails(name, args, "允许 MCP")
	}
	if len(e.ctrl.Writes) != 0 {
		t.Fatalf("controller writes while MCP is off: %v", e.ctrl.Writes)
	}
}

func TestTierGating(t *testing.T) {
	e := newEnv(t, nil)
	// Defaults: operate on, configure off.
	e.setAgent(rpc.AgentSettings{MCP: true, Operate: true})
	e.ok("status", nil)
	e.fails("layer_put", map[string]any{"id": "x", "content": "a: 1"}, "configure tool")
	e.fails("apply", nil, "configure tool")
	if e.h.Called(rpc.MethodLayerPut) || e.h.Called(rpc.MethodComposePreview) {
		t.Fatal("a refused configure tool reached the helper")
	}
	e.setAgent(rpc.AgentSettings{MCP: true})
	e.fails("set_mode", map[string]any{"mode": "direct"}, "operate tool")
	e.fails("tailscale_down", nil, "operate tool")
	e.ok("proxies", nil) // read stays on
	if len(e.ctrl.Writes) != 0 || len(e.ts.edits) != 0 {
		t.Fatalf("refused operate tools wrote: %v %v", e.ctrl.Writes, e.ts.edits)
	}
	e.h.Mu.Lock()
	n := len(e.h.Records)
	e.h.Mu.Unlock()
	if n != 0 {
		t.Fatalf("refused calls were recorded: %d", n)
	}
}

func TestReadOnlyEnv(t *testing.T) {
	e := newEnv(t, func(d *Deps) { d.ReadOnly = true })
	e.setAgent(rpc.AgentSettings{MCP: true, Operate: true, Configure: true})
	e.fails("set_tun", map[string]any{"enabled": false}, "TWINNEL_READ_ONLY")
	e.fails("apply", nil, "TWINNEL_READ_ONLY")
	e.ok("status", nil)
}

func TestHelperDown(t *testing.T) {
	e := newEnv(t, func(d *Deps) {
		d.Dial = func(context.Context) (Caller, func(), error) { return nil, nil, context.DeadlineExceeded }
		d.Controller, d.Secret = "", ""
	})
	m := e.ok("status", nil)
	if h := m["helper"].(map[string]any); h["reachable"] != false {
		t.Fatalf("helper = %v", h)
	}
	e.ok("tailnet_peers", nil)
	e.fails("proxies", nil, "helper is not reachable")
	e.fails("set_mode", map[string]any{"mode": "rule"}, "helper is not reachable")
}

func TestOperateRecordsAndUndo(t *testing.T) {
	e := newEnv(t, nil)
	m := e.ok("set_mode", map[string]any{"mode": "global"})
	if m["previous"] != "rule" || m["undo"] != "set_mode mode=rule" || e.ctrl.Mode != "global" {
		t.Fatalf("set_mode = %v (controller mode %s)", m, e.ctrl.Mode)
	}
	m = e.ok("select_node", map[string]any{"group": "Proxy", "node": "US 1"})
	if m["previous"] != "JP 1" || e.ctrl.Now["Proxy"] != "US 1" {
		t.Fatalf("select_node = %v", m)
	}
	e.fails("select_node", map[string]any{"group": "Proxy", "node": "Nope"}, "not a member")
	e.fails("select_node", map[string]any{"group": "Auto", "node": "JP 1"}, "only Selector")
	m = e.ok("set_exit_node", map[string]any{"peer": "exit-jp"})
	if m["previous"] != "exit-sg" || m["undo"] != "set_exit_node peer=exit-sg" {
		t.Fatalf("set_exit_node = %v", m)
	}
	e.fails("set_exit_node", map[string]any{"peer": "nas"}, "does not offer")
	if len(e.ts.edits) != 1 || e.ts.edits[0].ExitNodeID != "nSYNTH04CNTRL" {
		t.Fatalf("tailscale edits = %+v", e.ts.edits)
	}
	e.ok("flush_fakeip", nil)
	if e.ctrl.Flushes != 1 {
		t.Fatal("fake-ip cache not flushed")
	}
	m = e.ok("health_egress_run", nil)
	if ck, _ := m["check"].(map[string]any); ck["id"] != "leak-guard-egress" || ck["status"] != "ok" {
		t.Fatalf("health_egress_run = %v", m)
	}
	e.h.Mu.Lock()
	recs := append([]rpc.AgentAction(nil), e.h.Records...)
	e.h.Mu.Unlock()
	if len(recs) != 7 { // 5 ok + 2 failed select/exit attempts... counted below
		t.Logf("records: %+v", recs)
	}
	var tools []string
	for _, r := range recs {
		tools = append(tools, r.Tool)
		if r.Client != "test-agent 1.0" || r.Source != "agent" {
			t.Errorf("record %+v: want client test-agent 1.0, source agent", r)
		}
	}
	want := "set_mode select_node select_node select_node set_exit_node set_exit_node flush_fakeip health_egress_run"
	if strings.Join(tools, " ") != want {
		t.Fatalf("recorded %v, want %s", tools, want)
	}
	if !recs[0].OK || recs[0].Undo != "set_mode mode=rule" || recs[0].Args != "mode=global" {
		t.Fatalf("first record = %+v", recs[0])
	}
	if recs[2].OK || recs[2].Error == "" {
		t.Fatalf("failed select not recorded as failed: %+v", recs[2])
	}
	// Read tools are not recorded.
	e.ok("status", nil)
	e.h.Mu.Lock()
	defer e.h.Mu.Unlock()
	if len(e.h.Records) != len(recs) {
		t.Fatal("a read tool was recorded")
	}
}

func TestApplyPreviewsFirst(t *testing.T) {
	e := newEnv(t, nil)
	e.setAgent(rpc.AgentSettings{MCP: true, Operate: true, Configure: true})
	m := e.ok("apply", nil)
	if m["applied"] != false {
		t.Fatalf("apply without hash applied: %v", m)
	}
	p := m["preview"].(map[string]any)
	if !strings.Contains(p["diff"].(string), "example.org") {
		t.Fatalf("preview diff does not show the new rule: %v", p["diff"])
	}
	hash := p["preview_hash"].(string)
	if e.h.Applied != 0 {
		t.Fatal("apply ran without a hash")
	}
	// The overrides change after the preview: the old hash is refused.
	e.ok("layer_put", map[string]any{"id": "late", "content": "rules+:\n  - DOMAIN,example.net,DIRECT\n"})
	e.fails("apply", map[string]any{"preview_hash": hash}, "changed since that preview")
	if e.h.Applied != 0 {
		t.Fatal("apply ran with a stale hash")
	}
	m = e.ok("apply", nil)
	hash = m["preview"].(map[string]any)["preview_hash"].(string)
	m = e.ok("apply", map[string]any{"preview_hash": hash, "reason": "direct example.net"})
	if e.h.Applied != 1 || m["applied"] != true || m["history_id"] != "h-3" || m["rollback_id"] != "h-2" {
		t.Fatalf("apply = %v (applied %d)", m, e.h.Applied)
	}
}

func TestSettingsUpdateCannotWidenTiers(t *testing.T) {
	e := newEnv(t, nil)
	e.setAgent(rpc.AgentSettings{MCP: true, Configure: true})
	e.call("settings_update", map[string]any{"history_keep": 50, "agent": map[string]any{"operate": true, "configure": true}})
	e.h.Mu.Lock()
	defer e.h.Mu.Unlock()
	for _, u := range e.h.Updates {
		if u.Agent != nil {
			t.Fatalf("settings_update sent agent switches: %+v", u.Agent)
		}
	}
	if e.h.Agent.Operate {
		t.Fatal("operate turned itself on")
	}
}

func TestManagedLayersStayManaged(t *testing.T) {
	e := newEnv(t, nil)
	e.setAgent(rpc.AgentSettings{MCP: true, Configure: true})
	e.fails("layer_remove", map[string]any{"id": "coexist"}, "managed")
	e.fails("layer_put", map[string]any{"id": "coexist", "content": "dns: {}"}, "managed")
}

func TestAdminOnly(t *testing.T) {
	e := newEnv(t, nil)
	m := e.ok("admin_only", map[string]any{"action": "helper_uninstall"})
	if m["allowed"] != false || !strings.Contains(m["message"].(string), "停止使用 Twinnel") {
		t.Fatalf("admin_only = %v", m)
	}
}

func TestRedactYAML(t *testing.T) {
	out := RedactYAML(agenttest.LiveConfig)
	for _, s := range agenttest.Secrets {
		if strings.Contains(out, s) {
			t.Errorf("RedactYAML leaks %q:\n%s", s, out)
		}
	}
	for _, keep := range []string{"JP 1", "192.0.2.10", "sub.example.com", "DOMAIN-SUFFIX,example.com,Proxy"} {
		if !strings.Contains(out, keep) {
			t.Errorf("RedactYAML dropped %q:\n%s", keep, out)
		}
	}
	bad := "proxies:\n  - {name: a, password: " + agenttest.NodePassword + "\n  uuid: " + agenttest.NodeUUID + "\n"
	if out := RedactYAML(bad); strings.Contains(out, agenttest.NodePassword) || strings.Contains(out, agenttest.NodeUUID) {
		t.Errorf("RedactYAML of broken YAML leaks:\n%s", out)
	}
}

func TestRedactURLTokens(t *testing.T) {
	for in, want := range map[string]string{
		"probe https://www.gstatic.com/generate_204 ok":       "probe https://www.gstatic.com/generate_204 ok",
		"see https://github.com/MetaCubeX/mihomo/releases":    "see https://github.com/MetaCubeX/mihomo/releases",
		"GET " + agenttest.SubURL:                             "GET https://sub.example.com/api/v1/client/<redacted>?<redacted>",
		"https://x.example.com/s/0123456789abcdef0123/config": "https://x.example.com/s/<redacted>/config",
	} {
		s := &Server{secrets: map[string]bool{}}
		if got := s.redactString(in); got != want {
			t.Errorf("redactString(%q) = %q, want %q", in, got, want)
		}
	}
}

// importFile writes a synthetic file to import.
func importFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConfigImportPlansFirst(t *testing.T) {
	e := newEnv(t, nil)
	e.setAgent(rpc.AgentSettings{MCP: true, Operate: true})
	f := importFile(t, "nodes.yaml", "+rules:\n  - DOMAIN,example.net,DIRECT\n")
	e.fails("config_import", map[string]any{"path": f}, "configure tool")
	if e.h.Called(rpc.MethodConfigImport) {
		t.Fatal("a refused config_import reached the helper")
	}
	e.setAgent(rpc.AgentSettings{MCP: true, Operate: true, Configure: true})
	e.fails("config_import", map[string]any{"path": "nodes.yaml"}, "absolute")

	m := e.ok("config_import", map[string]any{"path": f})
	plan := m["plan"].(map[string]any)
	if m["imported"] != false || plan["conflicts"].(float64) != 1 || !strings.Contains(m["next"].(string), "on_conflict") {
		t.Fatalf("plan: %v", m)
	}
	e.h.Mu.Lock()
	dry := e.h.Imports[0].DryRun
	e.h.Mu.Unlock()
	if !dry {
		t.Fatal("the plan call was not a dry run")
	}
	e.fails("config_import", map[string]any{"path": f, "confirm": true}, "conflict")
	m = e.ok("config_import", map[string]any{"path": f, "confirm": true, "on_conflict": "rename"})
	if m["imported"] != true || m["undo"] != "layer_remove id=nodes-2" {
		t.Fatalf("import: %v", m)
	}
	e.noSecrets()
}

func TestComponentTools(t *testing.T) {
	e := newEnv(t, nil)
	e.setAgent(rpc.AgentSettings{MCP: true, Operate: false})
	e.fails("geo_update", nil, "operate tool")
	e.fails("dashboard_install", nil, "operate tool")
	e.setAgent(rpc.AgentSettings{MCP: true, Operate: true})
	e.call("geo_update", map[string]any{"proxy": "subscription"})
	e.call("dashboard_install", map[string]any{"proxy": "auto"})
	e.h.Mu.Lock()
	got := strings.Join(e.h.AccessoryUpdates, ",")
	e.h.Mu.Unlock()
	if got != "geo:subscription,ui:" {
		t.Errorf("updates %q", got)
	}
	out, _ := e.call("version", nil)
	if !strings.Contains(out, `"geo"`) || !strings.Contains(out, `"dashboard"`) {
		t.Errorf("version has no components: %s", out)
	}
}
