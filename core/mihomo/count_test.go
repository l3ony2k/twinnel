package mihomo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCountNodes(t *testing.T) {
	ps := map[string]Proxy{
		"DIRECT": {Type: "Direct"}, "REJECT": {Type: "Reject"}, "REJECT-DROP": {Type: "RejectDrop"},
		"PASS": {Type: "Pass"}, "COMPATIBLE": {Type: "Compatible"}, "GLOBAL": {Type: "Selector"},
		"Auto": {Type: "URLTest"}, "node-a": {Type: "Shadowsocks"}, "node-b": {Type: "Trojan"}, "node-c": {Type: "Vless"},
	}
	if n := CountNodes(ps); n != 3 {
		t.Fatalf("CountNodes = %d, want 3", n)
	}
}

func TestRuleCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rules" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"rules":[{"type":"DomainSuffix","payload":"example.com","proxy":"DIRECT"},{"type":"Match","payload":"","proxy":"GLOBAL"}]}`))
	}))
	defer srv.Close()
	c, err := New(Options{Address: strings.TrimPrefix(srv.URL, "http://")})
	if err != nil {
		t.Fatal(err)
	}
	n, err := c.RuleCount(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("RuleCount = %d, %v; want 2", n, err)
	}
}
