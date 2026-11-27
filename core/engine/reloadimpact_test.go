package engine

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

const impactBase = `mixed-port: 7899
allow-lan: false
bind-address: 127.0.0.1
external-controller: 127.0.0.1:9090
secret: ""
profile:
  store-selected: true
dns:
  enable: true
  listen: 127.0.0.1:53
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  fake-ip-filter: ['+.lan', '+.local']
  nameserver-policy:
    '+.lan': [system]
tun:
  enable: true
  device: utun
  stack: mixed
  auto-route: true
  dns-hijack: [any:53, tcp://any:53]
  route-exclude-address:
    - 10.0.0.0/8
    - 100.64.0.0/10
    - fc00::/7
    - fd7a:115c:a1e0::/48
proxy-groups:
  - {name: PROXY, type: select, proxies: [DIRECT]}
rules:
  - DOMAIN-SUFFIX,a.test,DIRECT
  - MATCH,PROXY
`

func edit(t *testing.T, s, old, new string) string {
	t.Helper()
	if !strings.Contains(s, old) {
		t.Fatalf("edit: %q not found", old)
	}
	return strings.Replace(s, old, new, 1)
}

func TestReloadImpact(t *testing.T) {
	cases := []struct {
		name  string
		new   func(string) string
		check func(t *testing.T, im rpc.ReloadImpact)
	}{
		{"identical", func(s string) string { return s }, func(t *testing.T, im rpc.ReloadImpact) {
			if im.Disruptive || im.Force || im.TUNRebuild || im.DNSListener || len(im.RestartNeeded) > 0 || len(im.Notes) > 0 {
				t.Errorf("%+v", im)
			}
		}},
		{"adopt-like: dns policy, fake-ip-filter, hosts, rules, tun list order", func(s string) string {
			s = edit(t, s, "    '+.lan': [system]\n", "    '+.lan': [system]\n    '+.ts.net': ['100.100.100.100#utun9']\n")
			s = edit(t, s, "['+.lan', '+.local']", "['+.lan', '+.local', '+.ts.net']")
			s = edit(t, s, "    - 10.0.0.0/8\n    - 100.64.0.0/10\n    - fc00::/7\n", "    - 10.0.0.0/8\n    - fc00::/7\n    - 100.64.0.0/10\n")
			s = edit(t, s, "rules:\n", "hosts:\n  nas: 100.64.0.2\nrules:\n  - DOMAIN,nas,DIRECT\n")
			s = edit(t, s, "secret: \"\"\n", "") // "" and missing are the same
			return s
		}, func(t *testing.T, im rpc.ReloadImpact) {
			if im.Disruptive || im.Force || im.TUNRebuild || im.DNSListener || len(im.RestartNeeded) > 0 {
				t.Errorf("%+v", im)
			}
		}},
		{"tun prefix spelled differently", func(s string) string {
			return edit(t, s, "    - fd7a:115c:a1e0::/48\n", "    - FD7A:115C:A1E0::/48\n")
		}, func(t *testing.T, im rpc.ReloadImpact) {
			if im.TUNRebuild {
				t.Errorf("%+v", im)
			}
		}},
		{"tun entry added", func(s string) string {
			return edit(t, s, "    - fc00::/7\n", "    - fc00::/7\n    - 192.168.0.0/16\n")
		}, func(t *testing.T, im rpc.ReloadImpact) {
			if !im.TUNRebuild || !im.Disruptive || im.Force {
				t.Errorf("%+v", im)
			}
		}},
		{"tun scalar changed", func(s string) string { return edit(t, s, "stack: mixed", "stack: gvisor") }, func(t *testing.T, im rpc.ReloadImpact) {
			if !im.TUNRebuild {
				t.Errorf("%+v", im)
			}
		}},
		{"fake-ip-range moves the TUN address", func(s string) string {
			return edit(t, s, "fake-ip-range: 198.18.0.1/16", "fake-ip-range: 198.19.0.1/16")
		}, func(t *testing.T, im rpc.ReloadImpact) {
			if !im.TUNRebuild {
				t.Errorf("%+v", im)
			}
		}},
		{"fake-ip-range prefix length only", func(s string) string {
			return edit(t, s, "fake-ip-range: 198.18.0.1/16", "fake-ip-range: 198.18.0.1/15")
		}, func(t *testing.T, im rpc.ReloadImpact) {
			if im.TUNRebuild {
				t.Errorf("%+v", im)
			}
		}},
		{"tun turned off", func(s string) string {
			s = edit(t, s, "  enable: true\n  device", "  enable: false\n  device")
			return edit(t, s, "stack: mixed", "stack: gvisor")
		}, func(t *testing.T, im rpc.ReloadImpact) {
			// old has TUN on, new has it off: closed
			if !im.TUNRebuild {
				t.Errorf("%+v", im)
			}
		}},
		{"tun off in both, tun fields differ", func(s string) string {
			s = edit(t, s, "  enable: true\n  device", "  enable: false\n  device")
			return s
		}, func(t *testing.T, im rpc.ReloadImpact) {
			old := ReloadImpact([]byte(edit(t, impactBase, "  enable: true\n  device", "  enable: false\n  device")), []byte(edit(t, edit(t, impactBase, "  enable: true\n  device", "  enable: false\n  device"), "stack: mixed", "stack: gvisor")))
			if old.TUNRebuild {
				t.Errorf("%+v", old)
			}
		}},
		{"dns listen", func(s string) string { return edit(t, s, "listen: 127.0.0.1:53", "listen: 0.0.0.0:53") }, func(t *testing.T, im rpc.ReloadImpact) {
			if !im.DNSListener || !im.Disruptive || im.TUNRebuild {
				t.Errorf("%+v", im)
			}
		}},
		{"mixed port", func(s string) string { return edit(t, s, "mixed-port: 7899", "mixed-port: 7898") }, func(t *testing.T, im rpc.ReloadImpact) {
			if !im.Force || !slices.Equal(im.Listeners, []string{"mixed-port"}) || !im.Disruptive {
				t.Errorf("%+v", im)
			}
		}},
		{"controller and secret", func(s string) string {
			s = edit(t, s, `secret: ""`, "secret: abc")
			return edit(t, s, "127.0.0.1:9090", "127.0.0.1:9091")
		}, func(t *testing.T, im rpc.ReloadImpact) {
			if im.Disruptive || im.Force || !slices.Equal(im.RestartNeeded, []string{"external-controller", "secret"}) {
				t.Errorf("%+v", im)
			}
		}},
		{"proxy providers", func(s string) string {
			return edit(t, s, "proxy-groups:", "proxy-providers:\n  sub: {type: http, url: 'http://127.0.0.1:1/x', path: ./sub.yaml}\n  local: {type: inline, payload: []}\nproxy-groups:")
		}, func(t *testing.T, im rpc.ReloadImpact) {
			if !im.Disruptive || !slices.Equal(im.ProviderConnsReset, []string{"sub"}) {
				t.Errorf("%+v", im)
			}
		}},
		{"store-selected off", func(s string) string { return edit(t, s, "store-selected: true", "store-selected: false") }, func(t *testing.T, im rpc.ReloadImpact) {
			if !im.SelectionReset || im.Disruptive {
				t.Errorf("%+v", im)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.check(t, ReloadImpact([]byte(impactBase), []byte(c.new(impactBase))))
		})
	}

	im := ReloadImpact([]byte("a: [unclosed"), []byte(impactBase))
	if !im.Force || !im.TUNRebuild || !im.Disruptive {
		t.Errorf("unparsable: %+v", im)
	}
}

// TWINNEL_IMPACT_A=live.yaml TWINNEL_IMPACT_B=composed.yaml go test -run RealReloadImpact -v ./core/engine/
func TestRealReloadImpact(t *testing.T) {
	a, b := os.Getenv("TWINNEL_IMPACT_A"), os.Getenv("TWINNEL_IMPACT_B")
	if a == "" || b == "" {
		t.Skip("set TWINNEL_IMPACT_A and TWINNEL_IMPACT_B")
	}
	ba, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	im := ReloadImpact(ba, bb)
	t.Logf("disruptive=%v force=%v tun_rebuild=%v dns_listener=%v listeners=%v providers=%v selection_reset=%v restart_needed=%v",
		im.Disruptive, im.Force, im.TUNRebuild, im.DNSListener, im.Listeners, im.ProviderConnsReset, im.SelectionReset, im.RestartNeeded)
	for _, n := range im.Notes {
		t.Logf("note: %s", n)
	}
}

func TestApplyReloadsWithoutForceWhenPortsUnchanged(t *testing.T) {
	h := newHarness(t)
	h.apply("first") // no file yet: force
	if f := h.ctrl.ForceFlags(); !slices.Equal(f, []bool{true}) {
		t.Fatalf("first apply force flags %v", f)
	}
	if _, err := h.e.PutLayer(rpc.LayerPutParams{ID: "more", Content: "+rules:\n  - DOMAIN-SUFFIX,example.net,DIRECT\n"}); err != nil {
		t.Fatal(err)
	}
	r := h.apply("rules only")
	if f := h.ctrl.ForceFlags(); len(f) != 2 || f[1] {
		t.Fatalf("rules-only apply force flags %v", f)
	}
	if r.Impact == nil || r.Impact.Disruptive || r.Impact.Force || r.Impact.TUNRebuild {
		t.Fatalf("impact %+v", r.Impact)
	}
	for _, w := range r.Warnings {
		if strings.Contains(w, "interrupts") {
			t.Errorf("warning %q", w)
		}
	}

	if _, err := h.e.PutLayer(rpc.LayerPutParams{ID: "port", Content: "mixed-port: 7999\n"}); err != nil {
		t.Fatal(err)
	}
	r = h.apply("port")
	if f := h.ctrl.ForceFlags(); len(f) != 3 || !f[2] {
		t.Fatalf("port apply force flags %v", f)
	}
	if r.Impact == nil || !r.Impact.Force || !slices.Contains(r.Impact.Listeners, "mixed-port") || !r.Impact.Disruptive {
		t.Fatalf("impact %+v", r.Impact)
	}
	found := false
	for _, w := range r.Warnings {
		found = found || strings.Contains(w, "interrupts the network briefly")
	}
	if !found {
		t.Errorf("no interruption warning: %v", r.Warnings)
	}
}

// mihomo keeps its controller secret until it restarts; the helper must
// keep reaching it after a reload put a new secret into the file.
func TestApplyAfterSecretChangeWithoutRestart(t *testing.T) {
	h := newHarness(t)
	h.apply("first")
	if err := h.e.Secrets.Set(store.SecretController, "rotated-secret"); err != nil {
		t.Fatal(err)
	}
	r, err := h.e.Apply(context.Background(), rpc.ApplyParams{Reason: "rotate", Force: true})
	if err != nil || !r.Reloaded {
		t.Fatalf("rotate: %+v %v", r, err)
	}
	if r.Impact == nil || !slices.Contains(r.Impact.RestartNeeded, "secret") || r.Impact.Disruptive {
		t.Fatalf("impact %+v", r.Impact)
	}
	if !strings.Contains(string(h.disk()), "rotated-secret") {
		t.Fatal("file does not carry the new secret")
	}
	// The fake still wants the old secret (like mihomo before a restart).
	if _, err := h.e.PutLayer(rpc.LayerPutParams{ID: "more", Content: "+rules:\n  - DOMAIN-SUFFIX,example.net,DIRECT\n"}); err != nil {
		t.Fatal(err)
	}
	r = h.apply("after rotate")
	if !r.Reloaded {
		t.Fatalf("not reloaded after rotation: %+v", r)
	}
	st, _ := h.e.Status(context.Background())
	if !st.Mihomo.Reachable {
		t.Fatalf("status: %+v", st.Mihomo)
	}
	// After a restart the new secret is the one that works.
	h.ctrl.Restart()
	if _, err := h.e.PutLayer(rpc.LayerPutParams{ID: "more2", Content: "+rules:\n  - DOMAIN-SUFFIX,example.com,DIRECT\n"}); err != nil {
		t.Fatal(err)
	}
	if r := h.apply("after restart"); !r.Reloaded {
		t.Fatalf("not reloaded after restart: %+v", r)
	}
}
