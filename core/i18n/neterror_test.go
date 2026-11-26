package i18n

import "testing"

func TestPlainNetError(t *testing.T) {
	cases := []struct{ raw, want string }{
		{`Get "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest": proxyconnect tcp: dial tcp 127.0.0.1:7890: connect: connection refused`,
			"没能连上 GitHub（经由代理 127.0.0.1:7890）：代理没有响应，mihomo 可能没在运行"},
		{`Get "https://pkgs.tailscale.com/stable/?mode=json": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`,
			"没能连上 pkgs.tailscale.com：连接超时"},
		{`Get "https://objects.githubusercontent.com/x": dial tcp: lookup objects.githubusercontent.com: no such host`,
			"没能连上 GitHub：找不到这个域名"},
		{`Get "https://example.test/sub": EOF`, "没能连上 example.test"},
		{`config: duplicate key "dns"`, ""},
		{`write /etc/x: permission denied`, ""},
	}
	for _, c := range cases {
		if got := PlainNetError(c.raw); got != c.want {
			t.Errorf("PlainNetError(%q)\n got %q\nwant %q", c.raw, got, c.want)
		}
	}
}
