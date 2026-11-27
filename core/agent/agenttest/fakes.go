// Package agenttest has fakes for testing Twinnel's MCP server: a helper
// (an rpc.Server on a temp socket with canned state) and a mihomo
// controller (httptest). Everything is synthetic; the fixture secrets are
// exported so tests can check that no output contains them.
package agenttest

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
)

// Fixture secrets: none of them may appear in any MCP output.
const (
	ControllerSecret = "ctl-secret-7f3a9c"
	NodePassword     = "node-pass-K2v9QxW"
	NodeUUID         = "3b8e2f1a-0c4d-4e5f-9a6b-7c8d9e0f1a2b"
	SubToken         = "subtoken-Zr8Pq3"
	SubURL           = "https://sub.example.com/api/v1/client/" + SubToken + "?flag=meta"
	QueryToken       = "qtok-55AA77"
)

// Secrets lists the fixture secrets.
var Secrets = []string{ControllerSecret, NodePassword, NodeUUID, SubToken, QueryToken}

// LiveConfig is the running config the fake helper returns (config.get live).
var LiveConfig = `mode: rule
mixed-port: 7890
secret: ` + ControllerSecret + `
tun:
  enable: true
  stack: mixed
dns:
  enable: true
  enhanced-mode: fake-ip
proxies:
  - {name: JP 1, type: ss, server: 192.0.2.10, port: 8388, cipher: aes-128-gcm, password: ` + NodePassword + `}
  - name: US 1
    type: vless
    server: 198.51.100.20
    port: 443
    uuid: ` + NodeUUID + `
proxy-providers:
  airport:
    type: http
    url: "` + SubURL + `"
    interval: 3600
proxy-groups:
  - {name: Proxy, type: select, proxies: [JP 1, US 1]}
rules:
  - DOMAIN-SUFFIX,example.com,Proxy
  - MATCH,DIRECT
`

// PreviewConfig adds a rule (config.get preview).
var PreviewConfig = strings.Replace(LiveConfig, "rules:\n", "rules:\n  - DOMAIN-SUFFIX,example.org,DIRECT\n", 1)

// LayerContent is the content of the stored override "nodes".
var LayerContent = `proxies:
  - name: HK 1
    type: trojan
    server: 203.0.113.5
    port: 443
    password: ` + NodePassword + `
proxy-providers:
  extra:
    type: http
    url: ` + SubURL + `
rules+:
  - DOMAIN,example.net,DIRECT
`

// Helper is the fake helper's state. Lock mu to read the recorded calls.
type Helper struct {
	Mu       sync.Mutex
	Socket   string
	Agent    rpc.AgentSettings
	Calls    []string // methods, in order
	Records  []rpc.AgentAction
	Updates  []rpc.SettingsUpdate
	Applied  int
	Layers   map[string]rpc.LayerContent
	Order    []string
	Hash     string // compose.preview hash
	Mode     string
	Ctrl     string // controller address for controller.credentials
	bus      *rpc.Bus
	srv      *rpc.Server
	listener interface{ Close() error }
}

// NewHelper starts a fake helper on a temp socket. ctrl is the controller
// address controller.credentials hands out.
func NewHelper(t testing.TB, ctrl string) *Helper {
	t.Helper()
	d, err := os.MkdirTemp("", "twinnelmcp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	h := &Helper{
		Socket: filepath.Join(d, "h.sock"), Ctrl: ctrl, Hash: "hash-1",
		Agent: rpc.AgentSettings{MCP: true, Operate: true},
		Layers: map[string]rpc.LayerContent{
			"nodes":   {Layer: rpc.Layer{ID: "nodes", Name: "My nodes", Stage: "user", Format: "merge", Enabled: true}, Content: LayerContent},
			"coexist": {Layer: rpc.Layer{ID: "coexist", Name: "Tailscale coexist", Stage: "coexist", Format: "merge", Enabled: true, Managed: true}, Content: "dns: {}\n"},
		},
		Order: []string{"nodes"},
		bus:   rpc.NewBus(64),
	}
	tr := rpc.NewUnixTransport()
	h.srv = rpc.NewServer(tr, rpc.AllowAll{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.register()
	l, err := tr.Listen(h.Socket)
	if err != nil {
		t.Fatal(err)
	}
	h.listener = l
	go h.srv.Serve(l)
	t.Cleanup(func() { l.Close(); h.srv.Close() })
	h.bus.Publish(rpc.EventComposeApplied, rpc.ApplyResult{Reason: "fetched " + SubURL, HistoryID: "h-1"})
	return h
}

// Dial connects to the fake helper.
func (h *Helper) Dial(ctx context.Context) (*rpc.Client, error) { return rpc.Dial(ctx, nil, h.Socket) }

// Called reports whether method was called.
func (h *Helper) Called(method string) bool {
	h.Mu.Lock()
	defer h.Mu.Unlock()
	for _, c := range h.Calls {
		if c == method {
			return true
		}
	}
	return false
}

func typed[P, R any](h *Helper, method string, fn func(p P) (R, error)) {
	rpc.Typed(h.srv, method, func(_ context.Context, p P) (R, error) {
		h.Mu.Lock()
		h.Calls = append(h.Calls, method)
		h.Mu.Unlock()
		return fn(p)
	})
}

func (h *Helper) register() {
	lock := func() func() { h.Mu.Lock(); return h.Mu.Unlock }
	typed(h, rpc.MethodSettingsGet, func(rpc.Empty) (rpc.Settings, error) {
		defer lock()()
		return rpc.Settings{Agent: h.Agent, AutoApply: true, TailscaleCoexist: true, LeakGuardGroups: []string{"Proxy"}, HistoryKeep: 30, KernelChannel: "stable"}, nil
	})
	typed(h, rpc.MethodSettingsUpdate, func(u rpc.SettingsUpdate) (rpc.Settings, error) {
		defer lock()()
		h.Updates = append(h.Updates, u)
		if u.Agent != nil && u.Agent.MCP != nil {
			h.Agent.MCP = *u.Agent.MCP
		}
		return rpc.Settings{Agent: h.Agent, HistoryKeep: 30}, nil
	})
	typed(h, rpc.MethodStatus, func(rpc.Empty) (rpc.Status, error) {
		return rpc.Status{HelperVersion: "0.9.0-test", ActiveSubscription: "main", Layers: 2,
			Mihomo:    rpc.MihomoStatus{Controller: h.Ctrl, Reachable: true, Version: "v1.19.34"},
			LastApply: &rpc.ApplyResult{Reason: "subscription " + SubURL, HistoryID: "h-1", Time: time.Unix(1_800_000_000, 0)}}, nil
	})
	typed(h, rpc.MethodControllerCredentials, func(rpc.Empty) (rpc.ControllerCredentials, error) {
		return rpc.ControllerCredentials{Controller: h.Ctrl, Secret: ControllerSecret, Source: "store"}, nil
	})
	report := json.RawMessage(`{"overall":"warn","checks":[{"id":"dns.tailnet","title":"Tailnet names","status":"warn","summary":"resolver at https://u:` + QueryToken + `@dns.example.com/q?key=` + QueryToken + `"},{"id":"mihomo.up","status":"ok","summary":"ok"}]}`)
	typed(h, rpc.MethodHealthLast, func(rpc.HealthLastParams) (rpc.HealthReport, error) {
		return rpc.HealthReport{Report: report, Reason: "scheduled", At: time.Unix(1_800_000_000, 0)}, nil
	})
	typed(h, rpc.MethodHealthEgress, func(rpc.HealthEgressParams) (rpc.HealthReport, error) {
		return rpc.HealthReport{Report: json.RawMessage(`{"overall":"ok","checks":[{"id":"leak-guard-egress","status":"ok","summary":"egress differs"}]}`), Reason: "egress"}, nil
	})
	typed(h, rpc.MethodHealthRun, func(rpc.HealthRunParams) (rpc.HealthReport, error) {
		return rpc.HealthReport{Report: report, Reason: "agent"}, nil
	})
	typed(h, rpc.MethodSubList, func(rpc.Empty) ([]rpc.Subscription, error) {
		return []rpc.Subscription{{ID: "main", Name: "Main", Active: true, HasURL: true, IntervalMinutes: 60,
			LastError: "GET " + SubURL + ": 502"}, {ID: "spare", Name: "Spare", HasURL: true}}, nil
	})
	typed(h, rpc.MethodSubPut, func(p rpc.SubPutParams) (rpc.Subscription, error) {
		if strings.Contains(p.URL, "bad") {
			return rpc.Subscription{}, rpc.ErrInvalid("cannot fetch %s", p.URL)
		}
		return rpc.Subscription{ID: p.ID, Name: p.Name, HasURL: p.URL != ""}, nil
	})
	typed(h, rpc.MethodSubActivate, func(rpc.IDParams) (rpc.Empty, error) { return rpc.Empty{}, nil })
	typed(h, rpc.MethodSubDelete, func(rpc.IDParams) (rpc.Empty, error) { return rpc.Empty{}, nil })
	typed(h, rpc.MethodSubRefresh, func(p rpc.IDParams) (rpc.SubRefreshResult, error) {
		return rpc.SubRefreshResult{Changed: true, Via: "proxy", Apply: &rpc.ApplyResult{Written: true, Reloaded: true, HistoryID: "h-2"}}, nil
	})
	typed(h, rpc.MethodLayerList, func(rpc.Empty) ([]rpc.Layer, error) {
		defer lock()()
		var out []rpc.Layer
		for _, id := range append(append([]string{}, h.Order...), "coexist") {
			if l, ok := h.Layers[id]; ok {
				out = append(out, l.Layer)
			}
		}
		return out, nil
	})
	typed(h, rpc.MethodLayerGet, func(p rpc.IDParams) (rpc.LayerContent, error) {
		defer lock()()
		l, ok := h.Layers[p.ID]
		if !ok {
			return l, rpc.ErrNotFound("layer", p.ID)
		}
		return l, nil
	})
	typed(h, rpc.MethodLayerPut, func(p rpc.LayerPutParams) (rpc.Layer, error) {
		defer lock()()
		l := rpc.Layer{ID: p.ID, Name: p.Name, Stage: "user", Format: "merge", Enabled: true, Size: len(p.Content)}
		if _, ok := h.Layers[p.ID]; !ok {
			h.Order = append(h.Order, p.ID)
		}
		h.Layers[p.ID] = rpc.LayerContent{Layer: l, Content: p.Content}
		h.Hash = "hash-" + p.ID
		return l, nil
	})
	typed(h, rpc.MethodLayerDelete, func(p rpc.IDParams) (rpc.Empty, error) {
		defer lock()()
		delete(h.Layers, p.ID)
		return rpc.Empty{}, nil
	})
	typed(h, rpc.MethodLayerSetEnabled, func(rpc.LayerEnableParams) (rpc.Empty, error) { return rpc.Empty{}, nil })
	typed(h, rpc.MethodLayerReorder, func(p rpc.ReorderParams) (rpc.Empty, error) {
		defer lock()()
		h.Order = p.IDs
		return rpc.Empty{}, nil
	})
	typed(h, rpc.MethodConfigGet, func(p rpc.ConfigGetParams) (rpc.ConfigText, error) {
		c := LiveConfig
		if p.Source == rpc.ConfigPreview {
			c = PreviewConfig
		}
		return rpc.ConfigText{Source: p.Source, ID: p.ID, Content: c, Hash: "cfg", Size: len(c)}, nil
	})
	typed(h, rpc.MethodComposePreview, func(rpc.Empty) (rpc.ComposeResult, error) {
		defer lock()()
		return rpc.ComposeResult{Applied: []string{"nodes"}, Hash: h.Hash, Valid: true, Changed: true}, nil
	})
	typed(h, rpc.MethodComposeApply, func(p rpc.ApplyParams) (*rpc.ApplyResult, error) {
		defer lock()()
		h.Applied++
		return &rpc.ApplyResult{ComposeResult: rpc.ComposeResult{Valid: true, Changed: true, Hash: h.Hash}, Reason: p.Reason, Written: true, Reloaded: true, HistoryID: "h-3"}, nil
	})
	typed(h, rpc.MethodHistoryList, func(rpc.Empty) ([]rpc.HistoryEntry, error) {
		return []rpc.HistoryEntry{{ID: "h-2", Kind: "applied", Reason: "subscription " + SubURL}, {ID: "h-1", Kind: "applied"}}, nil
	})
	typed(h, rpc.MethodHistoryRollback, func(p rpc.IDParams) (*rpc.ApplyResult, error) {
		return &rpc.ApplyResult{Written: true, Reloaded: true, HistoryID: "h-4"}, nil
	})
	typed(h, rpc.MethodKernelList, func(rpc.Empty) (rpc.KernelList, error) {
		return rpc.KernelList{Active: "/opt/twinnel/kernels/mihomo/current/mihomo", Current: "v1.19.34"}, nil
	})
	typed(h, rpc.MethodTailscaleInfo, func(rpc.Empty) (rpc.TailscaleInfo, error) {
		return rpc.TailscaleInfo{Installed: true, Version: "1.90.0"}, nil
	})
	typed(h, rpc.MethodTakeoverPlans, func(rpc.TakeoverPlansParams) ([]rpc.TakeoverPlan, error) {
		return []rpc.TakeoverPlan{{PlanID: "p1", State: rpc.PlanAdopted, ConfirmHash: "confirm-xyz", SecretSet: true, SubscriptionURL: true}}, nil
	})
	typed(h, rpc.MethodEventsWait, func(p rpc.EventsWaitParams) (rpc.EventsWaitResult, error) {
		evs, next := h.bus.Since(p.After)
		return rpc.EventsWaitResult{Events: evs, Next: next}, nil
	})
	typed(h, rpc.MethodAgentRecord, func(a rpc.AgentAction) (rpc.Empty, error) {
		defer lock()()
		a.Source = "agent"
		h.Records = append(h.Records, a)
		h.bus.Publish(rpc.EventAgentAction, a)
		return rpc.Empty{}, nil
	})
	typed(h, rpc.MethodAgentActivity, func(rpc.AgentActivityParams) ([]rpc.AgentAction, error) {
		defer lock()()
		out := make([]rpc.AgentAction, 0, len(h.Records))
		for i := len(h.Records) - 1; i >= 0; i-- {
			out = append(out, h.Records[i])
		}
		return out, nil
	})
}

// Controller is a fake mihomo controller.
type Controller struct {
	Mu      sync.Mutex
	Mode    string
	Tun     bool
	Now     map[string]string
	Writes  []string // "METHOD path"
	Flushes int
	BadAuth int
	Srv     *httptest.Server
}

// NewController starts a fake controller that wants ControllerSecret.
func NewController(t testing.TB) *Controller {
	c := &Controller{Mode: "rule", Tun: true, Now: map[string]string{"Proxy": "JP 1", "GLOBAL": "Proxy", "Auto": "US 1"}}
	c.Srv = httptest.NewServer(c)
	t.Cleanup(c.Srv.Close)
	return c
}

// Addr is the controller's host:port.
func (c *Controller) Addr() string { return c.Srv.Listener.Addr().String() }

func (c *Controller) proxies() map[string]any {
	hist := func(d int) []map[string]any { return []map[string]any{{"time": "2026-11-27T00:00:00Z", "delay": d}} }
	return map[string]any{
		"JP 1":   map[string]any{"name": "JP 1", "type": "Shadowsocks", "history": hist(120)},
		"US 1":   map[string]any{"name": "US 1", "type": "Vless", "history": hist(0)},
		"DIRECT": map[string]any{"name": "DIRECT", "type": "Direct"},
		"Proxy":  map[string]any{"name": "Proxy", "type": "Selector", "now": c.Now["Proxy"], "all": []string{"JP 1", "US 1", "DIRECT"}},
		"Auto":   map[string]any{"name": "Auto", "type": "URLTest", "now": c.Now["Auto"], "all": []string{"JP 1", "US 1"}},
		"GLOBAL": map[string]any{"name": "GLOBAL", "type": "Selector", "now": c.Now["GLOBAL"], "all": []string{"Proxy", "Auto", "DIRECT"}},
	}
}

func (c *Controller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+ControllerSecret {
		c.BadAuth++
		http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	enc := json.NewEncoder(w)
	p := r.URL.Path
	if r.Method != http.MethodGet {
		c.Writes = append(c.Writes, r.Method+" "+p)
	}
	switch {
	case p == "/version":
		enc.Encode(map[string]any{"meta": true, "version": "v1.19.34"})
	case p == "/configs" && r.Method == http.MethodGet:
		enc.Encode(map[string]any{"mode": c.Mode, "mixed-port": 7890, "tun": map[string]any{"enable": c.Tun}})
	case p == "/configs" && r.Method == http.MethodPatch:
		var body struct {
			Mode string          `json:"mode"`
			Tun  map[string]bool `json:"tun"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Mode != "" {
			c.Mode = body.Mode
		}
		if v, ok := body.Tun["enable"]; ok {
			c.Tun = v
		}
		w.WriteHeader(http.StatusNoContent)
	case p == "/proxies":
		enc.Encode(map[string]any{"proxies": c.proxies()})
	case strings.HasPrefix(p, "/proxies/") && strings.HasSuffix(p, "/delay"):
		enc.Encode(map[string]any{"delay": 88})
	case strings.HasPrefix(p, "/proxies/"):
		name := strings.TrimPrefix(p, "/proxies/")
		if r.Method == http.MethodPut {
			var body struct{ Name string }
			json.NewDecoder(r.Body).Decode(&body)
			c.Now[name] = body.Name
			w.WriteHeader(http.StatusNoContent)
			return
		}
		px, ok := c.proxies()[name]
		if !ok {
			http.Error(w, `{"message":"Resource not found"}`, http.StatusNotFound)
			return
		}
		enc.Encode(px)
	case strings.HasPrefix(p, "/group/"):
		enc.Encode(map[string]int{"JP 1": 90})
	case p == "/connections":
		enc.Encode(map[string]any{"uploadTotal": 10, "downloadTotal": 20, "connections": []map[string]any{
			{"id": "1", "upload": 1, "download": 2, "chains": []string{"JP 1", "Proxy"}, "rule": "DomainSuffix", "rulePayload": "example.com",
				"metadata": map[string]any{"host": "www.example.com", "process": "curl"}},
		}})
	case p == "/traffic":
		for i := 0; i < 2; i++ {
			enc.Encode(map[string]any{"up": 100, "down": 200, "upTotal": 1000, "downTotal": 2000})
			w.(http.Flusher).Flush()
		}
	case p == "/logs":
		enc.Encode(map[string]any{"type": "info", "payload": "[TCP] dial Proxy (match DomainSuffix/example.com) via " + SubURL})
		w.(http.Flusher).Flush()
	case p == "/cache/fakeip/flush":
		c.Flushes++
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	}
}
