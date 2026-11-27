package i18n

import (
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/msg"
	"github.com/l3ony2k/twinnel/core/rpc"
)

func TestWarningKnown(t *testing.T) {
	for in, want := range map[string]string{
		`proxy group "PROXY" not found`: "防泄漏：找不到策略组「PROXY」",
		`leak-guard: proxy group "A B" can still reach DIRECT through [x y] (nested groups are not changed: they may carry other traffic)`: "防泄漏：策略组「A B」仍能经由 x、y 走直连（嵌套策略组还承载别的流量，没有改动）",
		"generated a new controller secret (kept in the helper's secret store)":                                                            "已生成新的控制接口密钥",
		"3 subscription profiles found; adopt does not import a subscription URL (set one later with `twinnel sub put`)":                   "找到 3 个订阅配置；订阅地址不会导入，接管后在「订阅与覆写」添加",
		"external-controller: 0.0.0.0:9090 is reachable from the network (protected by the secret only)":                                   "external-controller：0.0.0.0:9090 可以从网络访问，只有密钥保护",
		"something new: keep me": "something new: keep me",
		"the subscription URL uses plain http: anyone on the network path can read it and replace the config it returns; use https if the provider offers it":                                           "订阅地址是明文 http，可能被读取或篡改；服务商支持的话请改用 https",
		"clients of the controller (the web dashboard, the app, scripts) need the new secret; it is in the helper's secret store (macOS: sudo security find-generic-password -s twinnel.controller -w)": "网页面板、脚本等客户端需要新密钥，可用 sudo security find-generic-password -s twinnel.controller -w 读取",
	} {
		if got := T.Warning(in); got != want {
			t.Errorf("Warning(%q)\n got %q\nwant %q", in, got, want)
		}
	}
	if WarningKind("x: this reload interrupts the network briefly: a") != "reload-disrupts" || WarningKind("nope") != "" {
		t.Error("WarningKind")
	}
	for _, l := range langs {
		for _, code := range msg.Codes() {
			if _, ok := l.S.Msg(rpc.Msg{Code: code}); !ok {
				t.Errorf("%s: no text for %s", l.Tag, code)
			}
		}
	}
}

// TestWarningsFromEngine runs the real layer transforms, so a reworded
// warning in core/config fails here instead of showing in English.
func TestWarningsFromEngine(t *testing.T) {
	src := `
dns:
  fake-ip-filter-mode: %s
  fake-ip-filter: ["+.ts.net", "*", "+.lan"]
proxies:
  - {name: home, type: direct}
proxy-groups:
  - {name: Guarded, type: select, proxies: [Nested, Dyn, home]}
  - {name: Dyn, type: select, include-all: true}
  - {name: Nested, type: select, proxies: [DIRECT]}
  - {name: Lonely, type: select, proxies: [home]}
`
	var got []string
	for _, mode := range []string{"whitelist", "odd"} {
		root, err := config.Parse([]byte(strings.Replace(src, "%s", mode, 1)))
		if err != nil {
			t.Fatal(err)
		}
		l, err := config.TailscaleCoexistLayer(config.TailnetInfo{MagicDNSSuffix: "example.ts.net"})
		if err != nil {
			t.Fatal(err)
		}
		w, err := l.Transform(root)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, w...)
		if mode == "whitelist" {
			w, err = config.LeakGuard([]string{"Guarded", "Lonely", "Missing"})(root)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, w...)
		}
	}
	if len(got) != 6 {
		t.Fatalf("warnings: %q", got)
	}
	for _, w := range got {
		if T.Warning(w) == w {
			t.Errorf("not translated: %q", w)
		}
	}
}
