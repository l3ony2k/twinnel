package fixture

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/health"
)

func runAll(t *testing.T, env health.Env) *health.Report {
	t.Helper()
	r := &health.Runner{Registry: health.DefaultRegistry(), Timeout: 5 * time.Second}
	return r.Run(context.Background(), env)
}

// The built-in machine is healthy: nothing fails or errors, so screenshots
// show a normal state unless a flow asks for another.
func TestSyntheticIsHealthy(t *testing.T) {
	s := health.DefaultSettings()
	s.Egress = true
	rep := runAll(t, &Env{M: Synthetic(), Prefs: s})
	if len(rep.Checks) == 0 {
		t.Fatal("no checks ran")
	}
	for _, c := range rep.Checks {
		t.Logf("%-28s %s %s", c.ID, c.Status, c.Summary)
		switch c.Status {
		case health.StatusFail:
			t.Errorf("%s: %s: %s", c.ID, c.Status, c.Summary)
		}
	}
}

func TestLoadRoundTrip(t *testing.T) {
	m := Synthetic()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "m.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tailscale.Self.HostName != "laptop" || got.Runtime.Proxies["PROXY"].Now != "node-hk-1" || got.EgressDirect != m.EgressDirect {
		t.Fatalf("round trip lost data: %+v", got.Tailscale.Self)
	}
}

func TestTailnetNames(t *testing.T) {
	e := &Env{M: Synthetic()}
	ctx := context.Background()
	for _, n := range []string{"nas", "nas.example-tailnet.ts.net", "NAS.example-tailnet.ts.net."} {
		r, err := e.LookupMihomo(ctx, n)
		if err != nil || r.Rcode != "NOERROR" || len(r.Addrs) != 1 || r.Addrs[0].String() != "100.64.0.2" {
			t.Errorf("%s: %+v %v", n, r, err)
		}
	}
	if r, _ := e.LookupSystem(ctx, "elsewhere.example"); r.Rcode != "NXDOMAIN" {
		t.Errorf("unknown name: %+v", r)
	}
	if r, _ := e.RouteGet(ctx, "100.64.0.2"); r.Interface != "utun9" {
		t.Errorf("tailnet route: %+v", r)
	}
	if r, _ := e.RouteGet(ctx, "203.0.113.1"); r.Interface != "utun5" {
		t.Errorf("default route: %+v", r)
	}
}
