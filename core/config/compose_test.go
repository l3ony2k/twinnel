package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestComposeOrder(t *testing.T) {
	sub := "mode: Rule\nrules: ['MATCH,PROXY']\n"
	layers := []Layer{
		// Given out of order on purpose: Compose sorts by stage.
		{ID: "guard", Kind: KindManaged, Stage: StageGuard, Enabled: true, Transform: func(root *yaml.Node) ([]string, error) {
			// The guard sees everything before it.
			rules := lookup(root, "rules")
			if got := len(rules.Content); got != 5 {
				t.Errorf("guard saw %d rules, want 5", got)
			}
			return []string{"ran"}, nil
		}},
		OverrideLayer("ts", "ts", KindManaged, StageCoexist, []byte("+rules: [ts]\n")),
		OverrideLayer("u1", "u1", KindUser, StageUser, []byte("+rules: [u1]\n")),
		OverrideLayer("u2", "u2", KindUser, StageUser, []byte("+rules: [u2]\n")),
		{ID: "off", Kind: KindUser, Stage: StageUser, Enabled: false, Override: []byte("+rules: [off]\n")},
		OverrideLayer("base", "base", KindManaged, StageBase, []byte("mode: rule\n+rules: [base]\n")),
	}
	r, err := ComposeReport([]byte(sub), layers)
	if err != nil {
		t.Fatal(err)
	}
	want := "mode: rule\nrules: [ts, u2, u1, base, 'MATCH,PROXY']\n"
	if g, w := canon(t, string(r.Config)), canon(t, want); g != w {
		t.Errorf("got:\n%s\nwant:\n%s", g, w)
	}
	if want := []string{"base", "u1", "u2", "ts", "guard"}; !reflect.DeepEqual(r.Applied, want) {
		t.Errorf("applied = %v, want %v", r.Applied, want)
	}
	if want := []string{"guard: ran"}; !reflect.DeepEqual(r.Warnings, want) {
		t.Errorf("warnings = %v", r.Warnings)
	}
}

func TestComposeErrors(t *testing.T) {
	tests := []struct {
		name    string
		sub     string
		layers  []Layer
		wantErr string
	}{
		{"bad subscription", "- a\n", nil, "subscription:"},
		{"duplicate id", "a: 1\n", []Layer{
			OverrideLayer("x", "", KindUser, StageUser, nil),
			OverrideLayer("x", "", KindUser, StageUser, nil),
		}, `duplicate layer ID "x"`},
		{"missing id", "a: 1\n", []Layer{OverrideLayer("", "n", KindUser, StageUser, nil)}, "has no ID"},
		{"bad layer yaml", "a: 1\n", []Layer{OverrideLayer("x", "", KindUser, StageUser, []byte("a: [\n"))}, "layer x:"},
		{"bad layer op", "a: 1\n", []Layer{OverrideLayer("x", "", KindUser, StageUser, []byte("+a: 1\n"))}, "layer x: a:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compose([]byte(tt.sub), tt.layers)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestComposeDedupe(t *testing.T) {
	sub := "dns:\n  fake-ip-filter: ['+.ts.net', '*.lan']\ntun:\n  route-exclude-address: [100.64.0.0/10]\nrules: [A, B, A]\n"
	over := "dns:\n  fake-ip-filter+: ['+.ts.net', '+.x']\ntun:\n  route-exclude-address+: [100.64.0.0/10]\n+rules: [B]\n"
	got, err := Compose([]byte(sub), []Layer{OverrideLayer("x", "", KindUser, StageUser, []byte(over))})
	if err != nil {
		t.Fatal(err)
	}
	want := "dns:\n  fake-ip-filter: ['+.ts.net', '*.lan', '+.x']\ntun:\n  route-exclude-address: [100.64.0.0/10]\nrules: [B, A]\n"
	if g, w := canon(t, string(got)), canon(t, want); g != w {
		t.Errorf("got:\n%s\nwant:\n%s", g, w)
	}
}

// Golden test of the whole pipeline on synthetic data.
func TestComposeGolden(t *testing.T) {
	info, err := TailnetInfoFromStatus(readTestdata(t, "tailscale-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	ts, err := TailscaleCoexistLayer(info)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ComposeReport(readTestdata(t, "subscription.yaml"), []Layer{
		OverrideLayer("base", "Base settings", KindManaged, StageBase, readTestdata(t, "base.yaml")),
		OverrideLayer("direct", "My direct rules", KindUser, StageUser, readTestdata(t, "user-direct.yaml")),
		ts,
		LeakGuardLayer([]string{"Google", "Missing"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "composed.golden.yaml")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, r.Config, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Config) != string(want) {
		t.Errorf("composed config differs from %s (UPDATE_GOLDEN=1 to refresh):\n%s", golden, r.Config)
	}
	wantWarn := []string{
		`leak-guard: proxy group "Google" can still reach DIRECT through [PROXY] (nested groups are not changed: they may carry other traffic)`,
		`leak-guard: proxy group "Missing" not found`,
	}
	if !reflect.DeepEqual(r.Warnings, wantWarn) {
		t.Errorf("warnings = %q", r.Warnings)
	}
}
