package i18n

import (
	"testing"

	"github.com/l3ony2k/duet/core/takeover"
)

func TestTakeoverCodes(t *testing.T) {
	for _, code := range []string{takeover.ChangeReordered, takeover.ChangeSubRuleDiffers, takeover.ChangeRulesReordered, takeover.ChangeTailscaleRules} {
		if _, ok := T.TakeoverChanges[code]; !ok {
			t.Errorf("change code %q has no text", code)
		}
	}
	for _, code := range []string{takeover.WhyCoexist, takeover.WhyCoexistPolicy, takeover.WhyCoexistFakeIP, takeover.WhyCoexistHosts, takeover.WhyCoexistUseHosts, takeover.WhyCoexistPrepended} {
		if _, ok := T.TakeoverWhys[code]; !ok {
			t.Errorf("why code %q has no text", code)
		}
	}
	for _, code := range []string{takeover.LayerNameBase, takeover.LayerNameCoexist, takeover.LayerNameLeakGuard} {
		if _, ok := T.TakeoverLayerNames[code]; !ok {
			t.Errorf("layer code %q has no text", code)
		}
	}
	cases := []struct{ got, want string }{
		{T.TakeoverChange(takeover.ChangeCounts, []string{"3", "0", "1"}, "x"), "新增 3 项，修改 1 项"},
		{T.TakeoverChange(takeover.ChangeNamedDiff, []string{"a, b", "", "c"}, "x"), "只在现有配置：a, b；有改动：c"},
		{T.TakeoverChange(takeover.ChangeTailscaleRules, []string{"2", "r1; r2"}, "x"), "2 条 Tailscale 规则改动：r1; r2"},
		{T.TakeoverChange("", nil, "dns.ipv6: true → false"), "dns.ipv6: true → false"},
		{T.TakeoverChange("future-code", []string{"1"}, "english"), "english"},
		{T.TakeoverWhy(takeover.WhyCoexistHosts, []string{"5"}, "x"), "Tailscale 共存：设备名在本地解析（5 台设备）"},
		{T.TakeoverWhy(takeover.WhyCoexistHosts, nil, "fallback"), "fallback"},
		{T.TakeoverLayerName(takeover.LayerNameBase, nil, "Base settings"), "基础设置"},
		{T.TakeoverLayerName(takeover.LayerNameLiveRules, []string{"DOMAIN,a.test,DIRECT", "0"}, "x"), "现有配置的规则：DOMAIN,a.test,DIRECT"},
		{T.TakeoverLayerName(takeover.LayerNameLiveRules, []string{"DOMAIN,a.test,DIRECT", "4"}, "x"), "现有配置的规则：DOMAIN,a.test,DIRECT 等 5 条"},
		{T.TakeoverLayerName("", nil, "# my rules"), "# my rules"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("%d: got %q want %q", i, c.got, c.want)
		}
	}
}
