package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fake is a minimal stand-in for mihomo's controller. All data is synthetic.
type fake struct {
	mu       sync.Mutex
	secret   string
	requests []string // "METHOD path?query body"
	proxies  map[string]Proxy
	mode     Mode
}

func newFake(t *testing.T, secret string) (*fake, *httptest.Server) {
	f := &fake{
		secret: secret,
		mode:   ModeRule,
		proxies: map[string]Proxy{
			"GLOBAL":   {Name: "GLOBAL", Type: "Selector", Now: "DIRECT", All: []string{"DIRECT", "Main", "Auto", "Video", "node-a", "node b/1"}},
			"Main":     {Name: "Main", Type: "Selector", Now: "Auto", All: []string{"Auto", "node-a", "node b/1", "DIRECT"}},
			"Auto":     {Name: "Auto", Type: "URLTest", Now: "node b/1", All: []string{"node-a", "node b/1"}},
			"Video":    {Name: "Video", Type: "Selector", Now: "Main", All: []string{"Main", "DIRECT"}},
			"Orphan":   {Name: "Orphan", Type: "Fallback", Now: "node-a", All: []string{"node-a"}},
			"node-a":   {Name: "node-a", Type: "Shadowsocks", Alive: true, History: []DelayRecord{{Delay: 120}, {Delay: 88}}},
			"node b/1": {Name: "node b/1", Type: "Trojan", Alive: true},
			"DIRECT":   {Name: "DIRECT", Type: "Direct", Alive: true},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, fmt.Sprintf("%s %s %s", r.Method, r.URL.RequestURI(), strings.TrimSpace(string(body))))
		f.mu.Unlock()
		if f.secret != "" && r.Header.Get("Authorization") != "Bearer "+f.secret {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Unauthorized"})
			return
		}
		f.serve(t, w, r, body)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fake) serve(t *testing.T, w http.ResponseWriter, r *http.Request, body []byte) {
	enc := json.NewEncoder(w)
	path := r.URL.Path
	switch {
	case path == "/version":
		_ = enc.Encode(Version{Meta: true, Version: "v9.9.9"})
	case path == "/configs" && r.Method == http.MethodGet:
		_ = enc.Encode(map[string]any{"mode": f.mode, "mixed-port": 7890, "tun": map[string]any{"enable": true, "device": "utun9", "route-exclude-address": []string{"100.64.0.0/10"}}})
	case path == "/configs" && r.Method == http.MethodPatch:
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		if s, ok := m["mode"].(string); ok {
			f.mode = Mode(s)
		}
		w.WriteHeader(http.StatusNoContent)
	case path == "/configs" && r.Method == http.MethodPut:
		w.WriteHeader(http.StatusNoContent)
	case path == "/proxies":
		_ = enc.Encode(map[string]any{"proxies": f.proxies})
	case strings.HasPrefix(path, "/proxies/") && strings.HasSuffix(path, "/delay"):
		name := strings.TrimSuffix(strings.TrimPrefix(path, "/proxies/"), "/delay")
		if name != "node b/1" {
			w.WriteHeader(http.StatusNotFound)
			_ = enc.Encode(map[string]string{"message": "resource not found"})
			return
		}
		if r.URL.Query().Get("url") == "" || r.URL.Query().Get("timeout") != "3000" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = enc.Encode(map[string]int{"delay": 42})
	case strings.HasPrefix(path, "/proxies/") && r.Method == http.MethodPut:
		w.WriteHeader(http.StatusNoContent)
	case strings.HasPrefix(path, "/group/"):
		_ = enc.Encode(map[string]int{"node-a": 10, "node b/1": 20})
	case path == "/traffic":
		fl := w.(http.Flusher)
		for i := 1; i <= 3; i++ {
			_ = enc.Encode(Traffic{Up: int64(i), Down: int64(i * 10), UpTotal: 100, DownTotal: 1000})
			fl.Flush()
		}
	case path == "/logs":
		fl := w.(http.Flusher)
		if r.URL.Query().Get("level") != "warning" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = enc.Encode(LogEntry{Type: "warning", Payload: "dial failed"})
		fl.Flush()
		<-r.Context().Done() // keep streaming until the client leaves
	case path == "/connections" && r.Method == http.MethodGet:
		_ = enc.Encode(Connections{DownloadTotal: 5, UploadTotal: 6, Connections: []Connection{{ID: "c1", Chains: []string{"node-a", "Main"}}}})
	case strings.HasPrefix(path, "/cache/") || strings.HasPrefix(path, "/connections/"):
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fake) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func client(t *testing.T, srv *httptest.Server, secret string, writes bool) *Client {
	c, err := New(Options{Address: srv.URL, Secret: secret, AllowWrites: writes})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewAddress(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "http://127.0.0.1:9090",
		"127.0.0.1:9091":         "http://127.0.0.1:9091",
		"http://[::1]:9090/":     "http://[::1]:9090",
		"https://example.test/x": "https://example.test/x",
	} {
		c, err := New(Options{Address: in})
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got := c.BaseURL(); got != want {
			t.Errorf("%q: base %q, want %q", in, got, want)
		}
	}
	if _, err := New(Options{Address: "http://"}); err == nil {
		t.Error("want error for empty host")
	}
}

func TestReads(t *testing.T) {
	ctx := context.Background()
	_, srv := newFake(t, "s3cret")
	c := client(t, srv, "s3cret", false)

	v, err := c.Version(ctx)
	if err != nil || v.Version != "v9.9.9" || !v.Meta {
		t.Fatalf("version %+v %v", v, err)
	}
	cfg, err := c.Configs(ctx)
	if err != nil || cfg.Mode != ModeRule || !cfg.Tun.Enable || cfg.Tun.Device != "utun9" || cfg.MixedPort != 7890 {
		t.Fatalf("configs %+v %v", cfg, err)
	}
	all, err := c.Proxies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range Groups(all) {
		names = append(names, g.Name)
	}
	if got := strings.Join(names, ","); got != "Main,Auto,Video,Orphan,GLOBAL" {
		t.Errorf("groups order %s", got)
	}
	if got := strings.Join(Resolve(all, "Video"), ">"); got != "Main>Auto>node b/1" {
		t.Errorf("resolve %s", got)
	}
	if d, ok := all["node-a"].LastDelay(); !ok || d != 88 {
		t.Errorf("last delay %d %v", d, ok)
	}
	if _, ok := all["node b/1"].LastDelay(); ok {
		t.Error("no history should report !ok")
	}
	conns, err := c.Connections(ctx)
	if err != nil || len(conns.Connections) != 1 || conns.Connections[0].Chains[1] != "Main" {
		t.Fatalf("connections %+v %v", conns, err)
	}
}

func TestDelayEscapesNames(t *testing.T) {
	f, srv := newFake(t, "")
	c := client(t, srv, "", false)
	d, err := c.Delay(context.Background(), "node b/1", "", 3*time.Second)
	if err != nil || d != 42 {
		t.Fatalf("delay %d %v", d, err)
	}
	if !strings.Contains(f.last(), "/proxies/node%20b%2F1/delay?") {
		t.Errorf("name not escaped: %s", f.last())
	}
	_, err = c.Delay(context.Background(), "missing", "", 3*time.Second)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 404 || ae.Message != "resource not found" {
		t.Errorf("want 404 APIError, got %v", err)
	}
	m, err := c.GroupDelay(context.Background(), "Auto", "", 0)
	if err != nil || m["node b/1"] != 20 {
		t.Errorf("group delay %v %v", m, err)
	}
}

func TestUnauthorized(t *testing.T) {
	_, srv := newFake(t, "right")
	c := client(t, srv, "wrong", false)
	_, err := c.Version(context.Background())
	if !IsUnauthorized(err) {
		t.Fatalf("want unauthorized, got %v", err)
	}
}

func TestWritesDisabled(t *testing.T) {
	f, srv := newFake(t, "")
	c := client(t, srv, "", false)
	ctx := context.Background()
	n := len(f.requests)
	for _, err := range []error{
		c.SelectProxy(ctx, "Main", "node-a"),
		c.SetMode(ctx, ModeGlobal),
		c.SetTun(ctx, false),
		c.Reload(ctx, ""),
		c.FlushFakeIP(ctx),
		c.FlushDNS(ctx),
		c.CloseConnection(ctx, "c1"),
	} {
		if !errors.Is(err, ErrWritesDisabled) {
			t.Errorf("want ErrWritesDisabled, got %v", err)
		}
	}
	if len(f.requests) != n {
		t.Error("a disabled write reached the server")
	}
}

func TestWrites(t *testing.T) {
	f, srv := newFake(t, "")
	c := client(t, srv, "", true)
	ctx := context.Background()
	cases := []struct {
		run  func() error
		want string
	}{
		{func() error { return c.SelectProxy(ctx, "Main", "node b/1") }, `PUT /proxies/Main {"name":"node b/1"}`},
		{func() error { return c.SetMode(ctx, ModeGlobal) }, `PATCH /configs {"mode":"global"}`},
		{func() error { return c.SetTun(ctx, false) }, `PATCH /configs {"tun":{"enable":false}}`},
		{func() error { return c.Reload(ctx, "/tmp/x.yaml") }, `PUT /configs?force=true {"path":"/tmp/x.yaml"}`},
		{func() error { return c.FlushFakeIP(ctx) }, `POST /cache/fakeip/flush `},
		{func() error { return c.FlushDNS(ctx) }, `POST /cache/dns/flush `},
		{func() error { return c.CloseConnection(ctx, "c1") }, `DELETE /connections/c1 `},
	}
	for _, tc := range cases {
		if err := tc.run(); err != nil {
			t.Fatalf("%s: %v", tc.want, err)
		}
		if got := f.last(); got != strings.TrimSpace(tc.want) && got != tc.want {
			t.Errorf("request %q, want %q", got, tc.want)
		}
	}
	if f.mode != ModeGlobal {
		t.Error("mode not applied")
	}
}

func TestStreamTraffic(t *testing.T) {
	_, srv := newFake(t, "")
	c := client(t, srv, "", false)
	var got []Traffic
	err := c.StreamTraffic(context.Background(), func(tr Traffic) { got = append(got, tr) })
	if !errors.Is(err, io.EOF) {
		t.Errorf("want EOF at end of stream, got %v", err)
	}
	if len(got) != 3 || got[2].Down != 30 || got[0].DownTotal != 1000 {
		t.Errorf("samples %+v", got)
	}
}

func TestStreamLogsCancel(t *testing.T) {
	_, srv := newFake(t, "")
	c := client(t, srv, "", false)
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan LogEntry, 1)
	done := make(chan error, 1)
	go func() { done <- c.StreamLogs(ctx, "warning", func(e LogEntry) { got <- e }) }()
	select {
	case e := <-got:
		if e.Type != "warning" || e.Payload != "dial failed" || e.Time.IsZero() {
			t.Errorf("entry %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no log entry")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("want context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not stop")
	}
}
