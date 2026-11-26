package config

import (
	"reflect"
	"testing"
)

func TestLeakGuard(t *testing.T) {
	sub := `proxy-groups:
  - {name: PROXY, type: select, proxies: [a, DIRECT]}
  - {name: Google, type: select, proxies: [DIRECT, a, b]}
  - {name: OnlyDirect, type: select, proxies: [DIRECT]}
  - {name: Provider, type: select, proxies: [DIRECT], use: [pv]}
  - {name: All, type: select, proxies: [DIRECT], include-all: true}
  - {name: Nested, type: select, proxies: [Mid, b]}
  - {name: Mid, type: select, proxies: [PROXY]}
  - {name: Cycle1, type: select, proxies: [Cycle2, DIRECT]}
  - {name: Cycle2, type: select, proxies: [Cycle1, c]}
  - {name: Untouched, type: select, proxies: [DIRECT]}
`
	groups := []string{"Google", "OnlyDirect", "Provider", "All", "Nested", "Cycle1", "Nope"}
	r, err := ComposeReport([]byte(sub), []Layer{LeakGuardLayer(groups)})
	if err != nil {
		t.Fatal(err)
	}
	want := `proxy-groups:
  - {name: PROXY, type: select, proxies: [a, DIRECT]}
  - {name: Google, type: select, proxies: [a, b]}
  - {name: OnlyDirect, type: select, proxies: [REJECT]}
  - {name: Provider, type: select, proxies: [], use: [pv]}
  - {name: All, type: select, proxies: [], include-all: true}
  - {name: Nested, type: select, proxies: [Mid, b]}
  - {name: Mid, type: select, proxies: [PROXY]}
  - {name: Cycle1, type: select, proxies: [Cycle2]}
  - {name: Cycle2, type: select, proxies: [Cycle1, c]}
  - {name: Untouched, type: select, proxies: [DIRECT]}
`
	if g, w := canon(t, string(r.Config)), canon(t, want); g != w {
		t.Errorf("got:\n%s\nwant:\n%s", g, w)
	}
	wantWarn := []string{
		`leak-guard: proxy group "OnlyDirect" has no members left; set to REJECT`,
		`leak-guard: proxy group "Nested" can still reach DIRECT through [Mid]`,
		`leak-guard: proxy group "Nope" not found`,
	}
	if !reflect.DeepEqual(r.Warnings, wantWarn) {
		t.Errorf("warnings:\n%q\nwant:\n%q", r.Warnings, wantWarn)
	}
}

func TestLeakGuardNoGroups(t *testing.T) {
	r, err := ComposeReport([]byte("mode: rule\n"), []Layer{LeakGuardLayer([]string{"G"})})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 1 {
		t.Errorf("warnings = %q", r.Warnings)
	}
	if _, err := Compose([]byte("proxy-groups: x\n"), []Layer{LeakGuardLayer([]string{"G"})}); err == nil {
		t.Error("proxy-groups scalar: no error")
	}
}
