package i18n

import (
	"fmt"
	"strings"
)

// TakeoverStrings word what the helper's takeover plan reports by code
// (core/takeover Change.Code, WhyCode, DerivedLayer.NameCode). The helper
// keeps the English texts for the CLI; a code Twinnel does not know here
// falls back to them.
type TakeoverStrings struct {
	// ChangeCounts parts, joined by ListSep; %s: a count.
	ChangeAdded, ChangeRemoved, ChangeChanged string
	// ChangeNamedDiff parts; %s: a list.
	ChangeOnlyLive, ChangeOnlyComposed, ChangeItemsChanged string
	ClauseSep, CountSep                                    string

	// Other change and reason codes → format (%s: the args in order).
	TakeoverChanges map[string]string
	TakeoverWhys    map[string]string

	// Generated layer names: code → format. live-rules has two forms, with
	// and without more rules after the first.
	TakeoverLayerNames                 map[string]string
	LayerLiveRulesOne, LayerLiveRulesN string
}

var zhTakeover = TakeoverStrings{
	ChangeAdded:        "新增 %s 项",
	ChangeRemoved:      "删除 %s 项",
	ChangeChanged:      "修改 %s 项",
	ChangeOnlyLive:     "只在现有配置：%s",
	ChangeOnlyComposed: "只在合成后：%s",
	ChangeItemsChanged: "有改动：%s",
	ClauseSep:          "；",
	CountSep:           "，",
	TakeoverChanges: map[string]string{
		"reordered":        "条目相同、顺序不同（include-all 策略组的优先级会变）",
		"sub-rule-differs": "子规则「%s」不同",
		"rules-reordered":  "目标相同的前部规则换了顺序；没有连接会因此走别的路线",
		"tailscale-rules":  "%s 条 Tailscale 规则改动：%s",
	},
	TakeoverWhys: map[string]string{
		"coexist":                   "由 Tailscale 共存覆写写入",
		"coexist-nameserver-policy": "Tailscale 共存：tailnet 的名字交给 %s 解析",
		"coexist-fake-ip-filter":    "Tailscale 共存：tailnet 的名字拿到真实地址，不用 fake-ip",
		"coexist-hosts":             "Tailscale 共存：设备名在本地解析（%s 台设备）",
		"coexist-use-hosts":         "Tailscale 共存：设备名要用到 hosts",
		"coexist-prepended":         "Tailscale 共存的规则在用户覆写之后前置，所以排在最前",
	},
	TakeoverLayerNames: map[string]string{
		"base":              "基础设置",
		"tailscale-coexist": "Tailscale 共存",
		"leak-guard":        "防泄漏",
	},
	LayerLiveRulesOne: "现有配置的规则：%s",
	LayerLiveRulesN:   "现有配置的规则：%s 等 %s 条",
}

// TakeoverChange words a plan change's What from its code and args, or
// returns fallback (the helper's English text, or a config diff line)
// when the code is empty or unknown.
func (s *Strings) TakeoverChange(code string, args []string, fallback string) string {
	switch code {
	case "counts":
		if len(args) != 3 {
			return fallback
		}
		return joinParts(args, s.CountSep, "0", s.ChangeAdded, s.ChangeRemoved, s.ChangeChanged)
	case "named-diff":
		if len(args) != 3 {
			return fallback
		}
		return joinParts(args, s.ClauseSep, "", s.ChangeOnlyLive, s.ChangeOnlyComposed, s.ChangeItemsChanged)
	}
	return format(s.TakeoverChanges, code, args, fallback)
}

// TakeoverWhy words a plan change's Why from its code and args.
func (s *Strings) TakeoverWhy(code string, args []string, fallback string) string {
	return format(s.TakeoverWhys, code, args, fallback)
}

// TakeoverLayerName words a generated layer name, or returns fallback (the
// helper's name: the person's own comment, or English).
func (s *Strings) TakeoverLayerName(code string, args []string, fallback string) string {
	if code == "live-rules" {
		switch {
		case len(args) == 2 && args[1] != "0":
			n := 0
			fmt.Sscan(args[1], &n)
			return fmt.Sprintf(s.LayerLiveRulesN, args[0], fmt.Sprint(n+1))
		case len(args) >= 1:
			return fmt.Sprintf(s.LayerLiveRulesOne, args[0])
		}
		return fallback
	}
	return format(s.TakeoverLayerNames, code, args, fallback)
}

// joinParts formats each arg that is not skip with its format.
func joinParts(args []string, sep, skip string, formats ...string) string {
	var parts []string
	for i, f := range formats {
		if args[i] != skip {
			parts = append(parts, fmt.Sprintf(f, args[i]))
		}
	}
	return strings.Join(parts, sep)
}

func format(m map[string]string, code string, args []string, fallback string) string {
	f, ok := m[code]
	if !ok || code == "" || strings.Count(f, "%s") != len(args) {
		return fallback
	}
	a := make([]any, len(args))
	for i, x := range args {
		a[i] = x
	}
	return fmt.Sprintf(f, a...)
}
