package health

import (
	"context"

	"github.com/l3ony2k/duet/core/i18n"
)

// User-facing texts of the health package: one table per language, chosen
// per run (WithLang), so the helper words a report in the language of the
// app that asked for it. zhTexts is the reference; strings_en.go is
// English. TestTextsComplete fails when a table misses a text.

// texts holds them.
type texts struct {
	titleReport      string
	labelOverall     string
	labelTime        string
	labelPlatform    string
	labelEvidence    string
	labelFix         string
	labelOK          string
	labelWarn        string
	labelFail        string
	labelSkip        string
	msgCounts        string
	msgNotOnPlatform string
	msgCheckPanicked string
	msgCheckTimeout  string

	// Check titles.
	titleTailnetRoute   string
	titleTUNExclude     string
	titleTailscaleRules string
	titleMagicDNS       string
	titlePeerNames      string
	titleSystemDNS      string
	titleVPNConflict    string
	titleDirectPeers    string
	titleLeakGuard      string
	titleController     string
	titleResolverBind   string
	titleFakeIPMode     string
	titleEgress         string

	// tailnet-resolver-binding
	msgBindNone      string
	msgBindOK        string
	msgBindHostsOnly string
	msgBindMissing   string
	msgBindStale     string
	msgBindNoQuad    string
	evBindIface      string
	evBindResolver   string
	evBindHosts      string
	evBindCurrent    string
	fixBind          string

	// fake-ip-filter-mode
	msgFakeIPNotUsed  string
	msgWildcardNone   string
	msgWildcardOK     string
	msgWildcardBad    string
	evFakeIPMode      string
	fixWildcard       string
	labelDefaultBlack string

	// leak-guard-egress
	msgEgressOff         string
	msgEgressErr         string
	msgEgressOK          string
	msgEgressSame        string
	msgEgressNotMeasured string
	msgEgressDirectPath  string
	evEgressDirect       string
	evEgressDirectErr    string
	evEgressSTUN         string
	evEgressProxied      string
	evEgressGroup        string
	evEgressURL          string
	fixEgress            string

	// Labels and evidence fragments.
	srcRuntime  string
	srcFile     string
	labelYes    string
	labelNo     string
	labelManual string
	labelDHCP   string
	// Linux only (NetworkService.DNSSource).
	labelMixed           string
	labelLinkDNS         string
	labelGlobalDNS       string
	msgReadFailed        string
	msgTSUnavailable     string
	msgTSNotRunning      string
	msgMihomoUnavailable string
	msgTUNOff            string
	msgNoRules           string

	// tailnet-route
	msgTSUserspace string
	msgTSNoIface   string
	msgRouteOK     string
	msgRouteBad    string
	evTSIface      string
	evIntoMihomo   string
	evNotTS        string
	evRouteEntry   string
	fixRouteBad    string

	// tun-route-exclude
	msgExcludeOK      string
	msgExcludeMissing string
	evExcludeList     string
	evCoveredBy       string
	fixExclude        string

	// tailscale-direct-rules
	msgRulesOK     string
	msgRulesBad    string
	msgRulesRisky  string
	evRuleCount    string
	msgRulesGlobal string
	msgRulesDirect string
	fixRulesGlobal string
	evNoMatch      string
	evNotDirect    string
	evShadowed     string
	fixRules       string

	// magicdns
	msgMagicDNSOff     string
	msgMagicOK         string
	msgMagicSysFail    string
	msgMagicMihomoFake string
	msgMagicMihomoNone string
	evSys              string
	evMihomo           string
	evNotTailnet       string
	evEmpty            string
	evDNSTimeout       string
	evResolverCfg      string
	evPolicy           string
	fixMagicSys        string
	fixMagicSystemLoop string
	fixMagicMihomo     string

	// peer-short-names
	msgNamesOK      string
	msgNamesNone    string
	msgNamesMissing string
	msgNamesStale   string
	evNamesPeers    string
	evNamesRules    string
	fixNames        string

	// system-dns
	msgMihomoDNSOff   string
	msgDNSOK          string
	msgDNSOKNoListen  string
	msgDNSBad         string
	msgDNSHijack      string
	msgDNSNoActive    string
	evSvc             string
	evSvcInactive     string
	evHijackCovers    string
	evExcludedDNS     string
	evDefaultResolver string
	fixDNS            string
	fixDNSLinux       string

	// vpn-conflict
	msgVPNOK          string
	msgVPNConnected   string
	msgVPNTailscaleNE string
	msgExitNode       string
	msgOtherTUN       string
	msgTUNNotDefault  string
	evConnected       string
	evTSNE            string
	evNoVPN           string
	evNoExitNode      string
	evDefaultRoute    string
	evNotMihomoTUN    string
	fixVPN            string

	// peer-direct
	msgPeersOK        string
	msgPeersRelayed   string
	msgPeersOffline   string
	msgPeersNone      string
	evPeerDirect      string
	evPeerRelayed     string
	evPeerIdle        string
	evPeerOffline     string
	evSelfEndpointTUN string
	fixPeers          string

	// leak-guard
	msgNoSensitive  string
	msgLeakOK       string
	msgLeakMode     string
	msgLeakNoGroup  string
	msgLeakSelected string
	msgLeakAuto     string
	msgLeakManual   string
	evLeakChain     string
	evLeakDirect    string
	evLeakAuto      string
	evLeakManual    string
	fixLeak         string

	// controller-security
	msgCtlOK       string
	msgCtlDisabled string
	msgCtlExposed  string
	msgCtlNoSecret string
	evSecretSet    string
	evSecretEmpty  string
	evCtlNoAuth    string
	fixCtl         string
}

var zhTexts = texts{
	titleReport:      "duet 健康检查报告",
	labelOverall:     "总体",
	labelTime:        "时间",
	labelPlatform:    "平台",
	labelEvidence:    "证据",
	labelFix:         "修复建议",
	labelOK:          "通过",
	labelWarn:        "警告",
	labelFail:        "失败",
	labelSkip:        "跳过",
	msgCounts:        "通过 %d · 警告 %d · 失败 %d · 跳过 %d",
	msgNotOnPlatform: "不适用于 %s",
	msgCheckPanicked: "检查自身出错（panic）",
	msgCheckTimeout:  "检查超时（%s）",

	// Check titles.
	titleTailnetRoute:   "tailnet 路由",
	titleTUNExclude:     "TUN 排除网段",
	titleTailscaleRules: "tailscaled 直连规则",
	titleMagicDNS:       "MagicDNS 解析",
	titlePeerNames:      "设备短名同步",
	titleSystemDNS:      "系统 DNS",
	titleVPNConflict:    "没有冲突的 VPN",
	titleDirectPeers:    "打洞质量",
	titleLeakGuard:      "防泄漏",
	titleController:     "控制接口安全",
	titleResolverBind:   "tailnet DNS 出口绑定",
	titleFakeIPMode:     "fake-ip 过滤模式",
	titleEgress:         "出口 IP 实测",

	// tailnet-resolver-binding
	msgBindNone:      "mihomo 没有把自己的连接绑定到物理接口，发往 100.100.100.100 的查询能进 Tailscale",
	msgBindOK:        "tailnet 解析器都带 #%s，查询走 Tailscale 接口",
	msgBindHostsOnly: "tailnet 解析器没带 #接口，但有设备 hosts：正向解析不受影响，反向解析和 hosts 之外的名字可能超时",
	msgBindMissing:   "mihomo 绑定了物理接口，tailnet 解析器却没带 #接口，也没有设备 hosts：tailnet 名字的查询会发到路由器，超时",
	msgBindStale:     "tailnet 解析器绑定的接口 %s 已不是 Tailscale 当前的接口 %s",
	msgBindNoQuad:    "tailnet 名字没交给 100.100.100.100（见 MagicDNS 检查）",
	evBindIface:      "绑定物理接口：%s",
	evBindResolver:   "nameserver-policy %s → %s",
	evBindHosts:      "hosts 里的设备名：%d 个",
	evBindCurrent:    "Tailscale 当前接口：%s",
	fixBind:          "重新生成「Tailscale 共存」覆写，让 tailnet 解析器写成 100.100.100.100#<Tailscale 接口>，并生成设备 hosts。tailscaled 重启后接口名会变，也需要重新生成。",

	// fake-ip-filter-mode
	msgFakeIPNotUsed:  "没有使用 fake-ip",
	msgWildcardNone:   "fake-ip-filter 里没有 '*'",
	msgWildcardOK:     "fake-ip-filter 的 '*' 配合 blacklist 模式：单标签名字拿真实地址",
	msgWildcardBad:    "fake-ip-filter 里有 '*'，但 fake-ip-filter-mode 是 %s：'*' 只在 blacklist 模式下表示“单标签名字不用 fake-ip”",
	evFakeIPMode:      "fake-ip-filter-mode：%s",
	fixWildcard:       "把 fake-ip-filter-mode 改回 blacklist（默认），或在当前模式下用对应写法排除单标签名字，然后重新生成「Tailscale 共存」覆写。",
	labelDefaultBlack: "blacklist（默认）",

	// leak-guard-egress
	msgEgressOff:         "未开启（duet doctor --egress）",
	msgEgressErr:         "出口测试失败：%v",
	msgEgressOK:          "敏感策略组的出口 IP 和本机直连出口不同：%s",
	msgEgressSame:        "经 mihomo 代理（%s）的出口 IP 和本机直连出口相同",
	msgEgressNotMeasured: "测试请求走的是 %s，敏感策略组当前用别的节点，没测到它们的出口",
	msgEgressDirectPath:  "测试请求本身被规则送去了 DIRECT，测不到代理出口",
	evEgressDirect:       "直连出口：%s",
	evEgressDirectErr:    "直连请求失败：%v（改用 tailscaled 的 STUN 结果）",
	evEgressSTUN:         "直连出口（tailscaled STUN）：%s",
	evEgressProxied:      "经 mihomo 出口：%s（链路：%s）",
	evEgressGroup:        "%s 当前节点：%s",
	evEgressURL:          "测试地址：%s（只发一次 GET，不带任何个人数据）",
	fixEgress:            "检查敏感策略组当前节点：节点的出口和本机相同，说明它没有真正代理（例如本地转发或 DIRECT 类节点）。",

	// Labels and evidence fragments.
	srcRuntime:  "运行中的 mihomo",
	srcFile:     "配置文件",
	labelYes:    "有",
	labelNo:     "没有",
	labelManual: "手动",
	labelDHCP:   "DHCP/默认",
	// Linux only (NetworkService.DNSSource).
	labelMixed:           "手动 + DHCP",
	labelLinkDNS:         "该网卡的 DNS，分不清手动还是 DHCP",
	labelGlobalDNS:       "系统全局 DNS",
	msgReadFailed:        "读取 %s 失败：%v",
	msgTSUnavailable:     "读不到 Tailscale 状态：%v",
	msgTSNotRunning:      "Tailscale 未在运行（BackendState=%s）",
	msgMihomoUnavailable: "读不到 mihomo 状态：%v",
	msgTUNOff:            "mihomo TUN 未启用",
	msgNoRules:           "mihomo 没有规则",

	// tailnet-route
	msgTSUserspace: "Tailscale 以 userspace 模式运行，没有 tun 接口",
	msgTSNoIface:   "找不到持有 Tailscale IP 的网络接口",
	msgRouteOK:     "tailnet 流量走 Tailscale 的 %s",
	msgRouteBad:    "这些 tailnet 地址没有走 Tailscale 的接口：%s",
	evTSIface:      "Tailscale 接口：%s；mihomo TUN：%s",
	evIntoMihomo:   "进了 mihomo TUN",
	evNotTS:        "不是 Tailscale 接口",
	evRouteEntry:   "路由表：%s → %s",
	fixRouteBad:    "在 mihomo 的 tun.route-exclude-address 里加入 100.64.0.0/10 和 fd7a:115c:a1e0::/48，然后重载 mihomo；如果仍不对，重启 tailscaled 让它重新写路由。",

	// tun-route-exclude
	msgExcludeOK:      "route-exclude-address 已包含 tailnet 网段",
	msgExcludeMissing: "route-exclude-address 缺少：%s",
	evExcludeList:     "route-exclude-address（%s）：%s",
	evCoveredBy:       "%s 被 %s 覆盖",
	fixExclude:        "在 tun.route-exclude-address 里加入缺少的网段（启用内置的「Tailscale 共存」覆写即可）。",

	// tailscale-direct-rules
	msgRulesOK:     "tailscaled、控制面、DERP 和 tailnet 网段都先命中 DIRECT",
	msgRulesBad:    "这些流量没有命中 DIRECT：%s",
	msgRulesRisky:  "前面有可能截走流量的非 DIRECT 规则：%s",
	evRuleCount:    "规则来源：%s，共 %d 条",
	msgRulesGlobal: "mihomo 处于全局模式（GLOBAL → %s）：规则不生效，tailscaled 和 tailnet 流量都走代理",
	msgRulesDirect: "mihomo 处于直连模式：所有流量直连，规则不生效",
	fixRulesGlobal: "把 mihomo 切回规则模式（mode: rule），或把 GLOBAL 选为 DIRECT。",
	evNoMatch:      "没有规则命中",
	evNotDirect:    "不是 DIRECT",
	evShadowed:     "前面的 %s 可能先命中",
	fixRules:       "把 PROCESS-NAME,tailscaled,DIRECT、DOMAIN-SUFFIX,tailscale.com,DIRECT、IP-CIDR,100.64.0.0/10,DIRECT,no-resolve 和 IP-CIDR6,fd7a:115c:a1e0::/48,DIRECT,no-resolve 放到规则最前面（启用「Tailscale 共存」覆写）。",

	// magicdns
	msgMagicDNSOff:     "MagicDNS 未开启",
	msgMagicOK:         "MagicDNS 名字通过系统和 mihomo 都解析到 tailnet 地址",
	msgMagicSysFail:    "系统解析 MagicDNS 名字出错：%s",
	msgMagicMihomoFake: "经 mihomo DNS 解析得到 fake-ip：%s",
	msgMagicMihomoNone: "mihomo DNS 解析不出：%s",
	evSys:              "系统   %s → %s",
	evMihomo:           "mihomo %s → %s",
	evNotTailnet:       "不是 tailnet 地址",
	evEmpty:            "（无结果）",
	evDNSTimeout:       "超时，2 秒内没有应答（mihomo 可能在等一个绕回自己的上游）",
	evResolverCfg:      "系统 resolver：%s → 100.100.100.100 %s；100.x 反解 zone %d 个",
	evPolicy:           "mihomo nameserver-policy %s：%s",
	fixMagicSys:        "确认 Tailscale 的 MagicDNS 已开启、tailscaled 在运行，然后重新生成「Tailscale 共存」覆写（它让 tailnet 名字跳过 fake-ip）。",
	fixMagicSystemLoop: "nameserver-policy 把 tailnet 域名交给了 system，而系统 DNS 指向 mihomo 自己，查询绕回来失败。",
	fixMagicMihomo:     "重新生成「Tailscale 共存」覆写：它把 tailnet 域名、单标签短名和 tailnet 反解交给 100.100.100.100（开了 auto-detect-interface / interface-name 时写成 100.100.100.100#<Tailscale 接口>），让短名跳过 fake-ip，并为设备生成 hosts。直接查询 127.0.0.1:53 或被 dns-hijack 劫持的程序才会受影响，走系统解析的程序不受影响。",

	// peer-short-names
	msgNamesOK:      "规则里的设备短名和当前 %d 台设备一致",
	msgNamesNone:    "规则里没有 tailnet 设备短名",
	msgNamesMissing: "规则里缺少：%s",
	msgNamesStale:   "规则里有已不存在的设备：%s",
	evNamesPeers:    "tailnet 设备：%s",
	evNamesRules:    "规则（%s）里的短名：%s",
	fixNames:        "重新生成「Tailscale 共存」覆写，让设备短名列表和当前 tailnet 一致。",

	// system-dns
	msgMihomoDNSOff:   "mihomo DNS 未启用",
	msgDNSOK:          "所有活动网络服务的 DNS 都指向 mihomo（%s）",
	msgDNSOKNoListen:  "所有活动网络服务的 DNS 都指向 127.0.0.1（mihomo 没有设置 dns.listen）",
	msgDNSBad:         "这些网络服务的 DNS 没有指向 mihomo：%s",
	msgDNSHijack:      "这些网络服务的 DNS 没有指向 mihomo，靠 dns-hijack 兜底：%s",
	msgDNSNoActive:    "没有活动的网络服务",
	evSvc:             "%s（%s）：%s［%s］",
	evSvcInactive:     "%s（%s）：未连接，跳过",
	evHijackCovers:    "dns-hijack 兜底",
	evExcludedDNS:     "该 DNS 在 route-exclude-address 内，dns-hijack 管不到",
	evDefaultResolver: "系统默认 resolver：%s",
	fixDNS:            "把这些网络服务的 DNS 设为 %s（networksetup -setdnsservers <服务名> <地址>）。",
	fixDNSLinux:       "把这些连接的 DNS 设为 %s。NetworkManager：nmcli connection modify <连接名> ipv4.dns %s ipv4.ignore-auto-dns yes，再 nmcli connection up <连接名>；只有 systemd-resolved 时：在 /etc/systemd/resolved.conf 写 DNS=%s 和 Domains=~.，再 systemctl restart systemd-resolved（resolvectl dns <网卡> <地址> 只到重启为止）。",

	// vpn-conflict
	msgVPNOK:          "没有其他 VPN 在运行，Tailscale 没用 exit node",
	msgVPNConnected:   "%s 已连接",
	msgVPNTailscaleNE: "%s 是 Tailscale 的 NetworkExtension 版本，会占用系统 VPN 通道",
	msgExitNode:       "Tailscale 正在使用 exit node %s",
	msgOtherTUN:       "默认路由在 %s 上，不是 mihomo 的 TUN",
	msgTUNNotDefault:  "mihomo TUN（%s）没有拿到默认路由（现在是 %s）",
	evConnected:       "已连接",
	evTSNE:            "Tailscale NetworkExtension 版",
	evNoVPN:           "没有系统 VPN 配置",
	evNoExitNode:      "Tailscale：没有使用 exit node",
	evDefaultRoute:    "1.1.1.1 的路由：%s",
	evNotMihomoTUN:    "不是 mihomo TUN",
	fixVPN:            "断开其他 VPN；需要 exit node 时先确认它和 mihomo TUN 的冲突。",

	// peer-direct
	msgPeersOK:        "%d 台直连，%d 台空闲（还没建立连接，无法判断）",
	msgPeersRelayed:   "这些设备正在经 DERP 中转：%s",
	msgPeersOffline:   "常用设备不在线：%s",
	msgPeersNone:      "没有在线的设备",
	evPeerDirect:      "%s：直连 %s",
	evPeerRelayed:     "%s：中转（%s）",
	evPeerIdle:        "%s：空闲（home DERP %s）",
	evPeerOffline:     "%s：离线",
	evSelfEndpointTUN: "本机 endpoint %s 是 mihomo TUN 地址（无害，但说明 tailscaled 看到了 TUN 接口）",
	fixPeers:          "常见原因：tailscaled 的 UDP 被 mihomo 接管（检查 PROCESS-NAME,tailscaled,DIRECT 规则）、路由器不支持 UPnP/NAT-PMP、对端是对称 NAT。可以先看 netcheck 结果。",

	// leak-guard
	msgNoSensitive:  "没有配置敏感策略组",
	msgLeakOK:       "敏感策略组（%s）都不会走 DIRECT",
	msgLeakMode:     "mihomo 处于 %s 模式，规则和策略组不生效",
	msgLeakNoGroup:  "找不到策略组 %s",
	msgLeakSelected: "%s 当前出口是 DIRECT",
	msgLeakAuto:     "%s 可能自动切到 DIRECT（%s）",
	msgLeakManual:   "%s 可以手动选到 DIRECT（%s）",
	evLeakChain:     "当前选择：%s",
	evLeakDirect:    "真实 IP 出口",
	evLeakAuto:      "自动回退路径：%s",
	evLeakManual:    "可选路径：%s",
	fixLeak:         "从敏感策略组里去掉 DIRECT，没有可用节点时用 REJECT（启用「防泄漏」覆写）。",

	// controller-security
	msgCtlOK:       "external-controller 只监听本机并设置了 secret",
	msgCtlDisabled: "没有开启 external-controller",
	msgCtlExposed:  "external-controller 监听在 %s，不只是本机",
	msgCtlNoSecret: "external-controller 没有设置 secret，本机任何程序都能控制 mihomo",
	evSecretSet:    "已设置",
	evSecretEmpty:  "空",
	evCtlNoAuth:    "不带 secret 的请求被接受",
	fixCtl:         "external-controller 只绑定 127.0.0.1，并设置随机 secret（由 helper 生成并存入钥匙串）。",
}

// textTables maps a language tag (core/i18n's) to its texts.
var textTables = map[string]*texts{
	"zh-Hans": &zhTexts,
	"en":      &enTexts,
}

// DefaultLang is the language of a run that names none.
const DefaultLang = "zh-Hans"

type langKey struct{}

// WithLang returns ctx with the language health texts are worded in, a
// tag core/i18n matched ("zh-Hans", "en"); unknown tags get DefaultLang.
func WithLang(ctx context.Context, tag string) context.Context {
	return context.WithValue(ctx, langKey{}, tag)
}

// LangOf returns the language WithLang set, or DefaultLang.
func LangOf(ctx context.Context) string {
	if tag, ok := ctx.Value(langKey{}).(string); ok {
		if m := i18n.Match(tag); textTables[m] != nil {
			return m
		}
	}
	return DefaultLang
}

// tx returns the texts of ctx's language.
func tx(ctx context.Context) *texts { return textFor(LangOf(ctx)) }

// textFor returns the texts of a language tag.
func textFor(tag string) *texts {
	if t, ok := textTables[tag]; ok {
		return t
	}
	return textTables[DefaultLang]
}
