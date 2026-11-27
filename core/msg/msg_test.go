package msg

import (
	"testing"

	"github.com/l3ony2k/twinnel/core/config"
)

// The layer errors come from the real merge, so a reworded one fails here.
func TestLayerErrorsFromCompose(t *testing.T) {
	for _, tt := range []struct {
		layer config.Layer
		code  string
		line  string
		key   string
	}{
		{config.OverrideLayer("app3-demo", "", config.KindUser, config.StageUser, []byte("hosts:\n  '+.lan': 1.2.3.4\n")), "layer-needs-list", "2", "+.lan"},
		{config.OverrideLayer("x", "", config.KindUser, config.StageUser, []byte("+rules: 1\n")), "layer-needs-list", "1", "+rules"},
	} {
		_, err := config.Compose([]byte("mixed-port: 1\n"), []config.Layer{tt.layer})
		if err == nil {
			t.Fatal("no error")
		}
		m := Parse(err.Error())
		if m.Code != tt.code || len(m.Args) < 3 || m.Args[0] != tt.layer.ID || m.Args[1] != tt.line || m.Args[2] != tt.key {
			t.Errorf("Parse(%q) = %+v", err, m)
		}
	}
	v, err := config.ImportVergeMerge([]byte("prepend-rules: 1\n"))
	if err == nil {
		t.Fatalf("verge: %s", v)
	}
	if m := Parse("layer v: " + err.Error()); m.Code != "layer-verge-list" || m.Args[1] != "1" || m.Args[2] != "prepend-rules" {
		t.Errorf("verge: %+v", m)
	}
}

func TestApplyErrors(t *testing.T) {
	for text, code := range map[string]string{
		"no active subscription":                                           "no-active-subscription",
		"subscription main has no content yet (refresh it first)":          "subscription-empty",
		"mihomo -t rejected the config: [bad]":                             "validate-failed",
		"reload: Put \"http://x/configs\": EOF":                            "reload-failed",
		"mihomo did not answer after reload: dial tcp: connection refused": "verify-failed",
		"write /etc/mihomo/config.yaml: permission denied":                 "write-failed",
		"leak-guard: proxy group \"PROXY\" not found":                      "group-missing",
		"x: proxy group \"A\" can still reach DIRECT through [b c] (nested groups are not changed: they may carry other traffic)": "group-direct",
		"takeover plan: live config: open /Users/x/.config/mihomo: no such file or directory":                                     "live-config-missing",
		"the live config is now /a, the plan was made for /b; re-plan":                                                            "replan",
		`subscription "home" has no URL`:                 "sub-no-url",
		"the running mihomo does not answer: EOF":        "mihomo-no-answer",
		"health checks are not available in this helper": "health-unavailable",
		"the controller belongs to another mihomo: 127.0.0.1:9090 is held by Clash Party; Twinnel did not reload it":                                  "controller-foreign-app",
		"the controller belongs to another mihomo: 127.0.0.1:9090 is held by another program; Twinnel did not reload it":                              "controller-foreign",
		"the config did not change, but 127.0.0.1:9090 is held by Clash Party's mihomo; Twinnel's mihomo is not the one running":                      "controller-foreign-idle-app",
		"the config did not change, but 127.0.0.1:9090 is held by another program's mihomo; Twinnel's mihomo is not the one running":                  "controller-foreign-idle",
		"reload: PUT /configs?force=true: 400 Bad Request: path is not subpath of home directory or SAFE_PATHS: /x/config.yaml\n allowed paths: [/y]": "reload-safe-paths",
		"something else": "",
	} {
		if m := Parse(text); m.Code != code || m.Text != text {
			t.Errorf("Parse(%q) = %+v, want %s", text, m, code)
		}
	}
	if m := Parse(`proxy group "A" can still reach DIRECT through [b c]`); len(m.Args) != 2 || m.Args[1] != "b\nc" {
		t.Errorf("list arg: %+v", m)
	}
}
