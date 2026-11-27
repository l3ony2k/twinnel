package health

import "context"

// FixText is what the user reads about one reason: the problem in plain
// words (Plain) and, by the fix's kind, what the one-click fix does, the
// one thing to change where the guided fix leads, or why Twinnel cannot
// fix it and what to do instead (How).
type FixText struct {
	Plain string `json:"plain"`
	How   string `json:"how"`
}

// Fix texts per language, keyed by reason; "reason@platform" wins over
// the plain key. TestEveryReasonHasAFix checks both tables cover every
// reason.
var zhFixTexts = map[string]FixText{
	ReasonReadFailed: {"读取系统信息时出错，这次没能检查完。", "重新检查一次；如果一直出错，复制报告反馈给我们。"},
	ReasonCheckTimeout: {"检查用了太久，没等到结果。",
		"重新检查一次。网络刚切换或 mihomo 正忙时常见，多半下次就好。"},
	ReasonCheckPanicked: {"这项检查自己出错了，不代表你的网络有问题。",
		"这是 Twinnel 的问题，不是设置能修的。重新检查一次；仍然出错就复制报告反馈给我们。"},

	ReasonRouteNoIface: {"Tailscale 在运行，但系统里找不到它的网卡。",
		"重启 tailscaled，让它重新建网卡和路由。期间 tailnet 连接会断几秒。"},
	ReasonRouteLookup: {"有的 tailnet 地址查不到路由，这次没法判断。", "重新检查一次。"},
	ReasonRouteIntoMihomo: {"发往 tailnet 设备的流量进了 mihomo，而不是 Tailscale。",
		"重新生成「Tailscale 共存」覆写并应用：把 tailnet 网段排除出 mihomo 的 TUN。"},
	ReasonRouteElsewhere: {"发往 tailnet 设备的流量走了别的网卡，没进 Tailscale。",
		"重启 tailscaled，让它把路由重新写一遍。期间 tailnet 连接会断几秒。"},

	ReasonExcludeMissing: {"mihomo 的 TUN 会把 tailnet 流量也抢过去。",
		"重新生成「Tailscale 共存」覆写并应用：它在 TUN 的排除网段里加上 tailnet。"},

	ReasonRulesGlobal: {"mihomo 在全局模式，Tailscale 自己的连接也被送去了代理。",
		"把 mihomo 切回规则模式。随时可以在概览里切回来。"},
	ReasonRulesNotDirect: {"Tailscale 的连接会被规则送去代理，tailnet 可能连不上或很慢。",
		"重新生成「Tailscale 共存」覆写并应用：把 Tailscale 的直连规则放到最前面。"},
	ReasonRulesShadowed: {"前面有规则可能抢先截走 Tailscale 的连接。",
		"重新生成「Tailscale 共存」覆写并应用：把 Tailscale 的直连规则放到最前面。"},

	ReasonMagicNoResolver: {"系统没有把 tailnet 名字交给 Tailscale 解析。",
		"Twinnel 不改 Tailscale 自己的设置。在 Tailscale 里打开「使用 Tailscale DNS」，或在终端运行 tailscale set --accept-dns=true。"},
	ReasonMagicNoResolver + "@" + Windows: {"系统没有把 tailnet 名字交给 Tailscale 解析。",
		"Twinnel 不改 Tailscale 自己的设置。在任务栏的 Tailscale 菜单里勾选「Use Tailscale DNS settings」，或运行 tailscale set --accept-dns=true。"},
	ReasonMagicNoResolver + "@" + Linux: {"系统没有把 tailnet 名字交给 Tailscale 解析。",
		"Twinnel 不改 Tailscale 自己的设置。运行 sudo tailscale set --accept-dns=true；没有 systemd-resolved 的系统还需要它来接管 DNS。"},
	ReasonMagicSystem: {"用 tailnet 设备名访问时，系统解析出的不是设备的地址。",
		"重新生成「Tailscale 共存」覆写并应用：tailnet 名字直接交给 Tailscale 解析，不用 fake-ip。"},
	ReasonMagicMihomo: {"经 mihomo 解析 tailnet 设备名会得到假地址或没有结果。",
		"重新生成「Tailscale 共存」覆写并应用：tailnet 名字交给 Tailscale 解析，并为设备写好 hosts。"},

	ReasonNamesNone: {"规则里还没有 tailnet 设备的短名，用短名访问设备会走代理。",
		"重新生成「Tailscale 共存」覆写并应用：按当前设备写入短名规则。"},
	ReasonNamesOutdated: {"规则里的设备短名和现在的 tailnet 对不上。",
		"重新生成「Tailscale 共存」覆写并应用：按当前设备更新短名规则。"},

	ReasonBindStale: {"tailnet 名字的查询绑在 Tailscale 旧的网卡上，会超时。",
		"重新生成「Tailscale 共存」覆写并应用：绑定到 Tailscale 现在的网卡。"},
	ReasonBindMissing: {"tailnet 名字的查询会从物理网卡发出，到不了 Tailscale。",
		"重新生成「Tailscale 共存」覆写并应用：把查询绑定到 Tailscale 的网卡，并写好设备 hosts。"},

	ReasonFakeIPWildcard: {"fake-ip 过滤的模式不是 blacklist，单标签名字会拿到假地址。",
		"在设置 fake-ip-filter-mode 的覆写里，把它改回 blacklist（或删掉这一行），保存并应用。"},

	ReasonDNSNotMihomo: {"有网络连接的 DNS 没交给 mihomo，分流和防泄漏对它不起作用。",
		"把这些连接的 DNS 指向 mihomo。会先备份原来的设置，可以撤销。"},
	ReasonDNSViaHijack: {"有网络连接的 DNS 没交给 mihomo，现在靠 TUN 劫持兜底。",
		"把这些连接的 DNS 直接指向 mihomo。会先备份原来的设置，可以撤销。"},
	ReasonDNSNotMihomo + "@" + Linux: {"有网络连接的 DNS 没交给 mihomo，分流和防泄漏对它不起作用。",
		"Linux 上 Twinnel 还不能替你改系统 DNS。在网络设置里把这个连接的 DNS 设为 127.0.0.1 并关掉自动 DNS，或用 nmcli（命令见详情）。"},
	ReasonDNSViaHijack + "@" + Linux: {"有网络连接的 DNS 没交给 mihomo，现在靠 TUN 劫持兜底。",
		"Linux 上 Twinnel 还不能替你改系统 DNS。在网络设置里把这个连接的 DNS 设为 127.0.0.1 并关掉自动 DNS，或用 nmcli（命令见详情）。"},

	ReasonVPNOther: {"另一个 VPN 已连接，它和 mihomo 抢同一条路。",
		"这个 VPN 是你另外装的应用，Twinnel 不去断开别人的连接。在那个 VPN 应用里断开，或在 系统设置 → VPN 里关掉它。"},
	ReasonVPNOther + "@" + Windows: {"另一个 VPN 已连接，它和 mihomo 抢同一条路。",
		"这个 VPN 是你另外装的应用，Twinnel 不去断开别人的连接。在那个 VPN 应用里断开，或在 设置 → 网络和 Internet → VPN 里断开。"},
	ReasonVPNOther + "@" + Linux: {"另一个 VPN 已连接，它和 mihomo 抢同一条路。",
		"这个 VPN 是你另外装的应用，Twinnel 不去断开别人的连接。在那个 VPN 应用或网络设置里断开（nmcli connection down <名字>）。"},
	ReasonVPNTailscale: {"装的是 App Store 版 Tailscale，它占着系统的 VPN 通道。",
		"这个版本由系统管理，Twinnel 改不了。退出它并改用开源版 tailscaled（Twinnel 的设置向导能装），或接受这个冲突。"},
	ReasonVPNExitNode: {"Tailscale 正在用出口节点，它和 mihomo 的 TUN 都想接管全部流量。",
		"停用出口节点。之后在 Tailscale 页可以随时再选回来。"},
	ReasonVPNOtherTUN: {"另一个隧道接管了默认路由，流量没有经过 mihomo。",
		"这个隧道属于别的 VPN 应用，Twinnel 不去关它。在那个应用里断开，或在 系统设置 → VPN 里关掉。"},
	ReasonVPNOtherTUN + "@" + Windows: {"另一个隧道接管了默认路由，流量没有经过 mihomo。",
		"这个隧道属于别的 VPN 应用，Twinnel 不去关它。在那个应用里断开，或在 设置 → 网络和 Internet → VPN 里断开。"},
	ReasonVPNOtherTUN + "@" + Linux: {"另一个隧道接管了默认路由，流量没有经过 mihomo。",
		"这个隧道属于别的 VPN 应用，Twinnel 不去关它。在那个应用或网络设置里断开（nmcli connection down <名字>，wg-quick down <接口>）。"},
	ReasonVPNNotDefault: {"mihomo 的 TUN 开着，却没拿到默认路由，流量没经过它。",
		"重启 mihomo，让它重新接管默认路由。期间网络会断几秒。"},

	ReasonLeakMode: {"mihomo 不在规则模式，防泄漏的策略组不起作用。", "把 mihomo 切回规则模式。"},
	ReasonLeakNoGroup: {"要保护的策略组在当前配置里不存在。",
		"在「防泄漏」里选上你配置里真正要保护的策略组。"},
	ReasonLeakSelected: {"敏感策略组现在直接用本机网络出去。",
		"打开「防泄漏」覆写并应用：从这些策略组里去掉 DIRECT，没节点可用时宁可断开。"},
	ReasonLeakMayGo: {"敏感策略组可能切到 DIRECT，用本机网络出去。",
		"打开「防泄漏」覆写并应用：从这些策略组里去掉 DIRECT，没节点可用时宁可断开。"},

	ReasonEgressFailed: {"经 mihomo 的测试请求没成功，没测到出口。", "再测一次。仍然失败多半是节点不可用，换一个节点再测。"},
	ReasonEgressSame: {"走代理出去的 IP 和本机一样，这个节点实际没有代理。",
		"在节点页给这个策略组换一个节点，然后再测一次出口。"},

	ReasonCtlExposed: {"mihomo 的控制接口对局域网开放，别的设备可能控制它。",
		"加一个覆写把控制接口只绑定到本机（127.0.0.1）并应用。可以撤销。"},
	ReasonCtlNoSecret: {"mihomo 的控制接口没有密钥，本机任何程序都能控制它。",
		"生成随机密钥并应用，密钥存在后台助手里。网页面板等其他工具之后需要新密钥。"},
}

var enFixTexts = map[string]FixText{
	ReasonReadFailed:    {"Reading the system failed, so this check did not finish.", "Check again. If it keeps failing, copy the report and send it to us."},
	ReasonCheckTimeout:  {"The check took too long and gave no result.", "Check again. This is common right after a network change or while mihomo is busy."},
	ReasonCheckPanicked: {"This check itself broke; it says nothing about your network.", "This is a Twinnel bug, not a setting. Check again; if it still breaks, copy the report and send it to us."},

	ReasonRouteNoIface:    {"Tailscale runs, but its network adapter is missing.", "Restart tailscaled so it creates its adapter and routes again. The tailnet drops for a few seconds."},
	ReasonRouteLookup:     {"Some tailnet addresses have no route to look up, so this could not be judged.", "Check again."},
	ReasonRouteIntoMihomo: {"Traffic to tailnet devices goes into mihomo instead of Tailscale.", "Regenerate the Tailscale coexistence override and apply it: it keeps the tailnet out of mihomo's TUN."},
	ReasonRouteElsewhere:  {"Traffic to tailnet devices leaves through another adapter, not Tailscale.", "Restart tailscaled so it writes its routes again. The tailnet drops for a few seconds."},

	ReasonExcludeMissing: {"mihomo's TUN would take tailnet traffic too.", "Regenerate the Tailscale coexistence override and apply it: it adds the tailnet to the TUN's excluded ranges."},

	ReasonRulesGlobal:    {"mihomo is in global mode, so Tailscale's own connections go through the proxy.", "Switch mihomo back to rule mode. You can switch it again on the overview."},
	ReasonRulesNotDirect: {"Rules send Tailscale's connections through the proxy; the tailnet may fail or be slow.", "Regenerate the Tailscale coexistence override and apply it: Tailscale's direct rules go first."},
	ReasonRulesShadowed:  {"An earlier rule may catch Tailscale's connections first.", "Regenerate the Tailscale coexistence override and apply it: Tailscale's direct rules go first."},

	ReasonMagicNoResolver:                 {"The system does not ask Tailscale for tailnet names.", "Twinnel does not change Tailscale's own settings. Turn on Use Tailscale DNS in Tailscale, or run tailscale set --accept-dns=true."},
	ReasonMagicNoResolver + "@" + Windows: {"The system does not ask Tailscale for tailnet names.", "Twinnel does not change Tailscale's own settings. In the Tailscale menu in the taskbar, check Use Tailscale DNS settings, or run tailscale set --accept-dns=true."},
	ReasonMagicNoResolver + "@" + Linux:   {"The system does not ask Tailscale for tailnet names.", "Twinnel does not change Tailscale's own settings. Run sudo tailscale set --accept-dns=true; without systemd-resolved, Tailscale also needs to manage DNS."},
	ReasonMagicSystem:                     {"Tailnet device names resolve to the wrong address on this computer.", "Regenerate the Tailscale coexistence override and apply it: tailnet names go to Tailscale, without fake-ip."},
	ReasonMagicMihomo:                     {"Through mihomo, tailnet device names get a fake address or no answer.", "Regenerate the Tailscale coexistence override and apply it: tailnet names go to Tailscale, and devices get hosts entries."},

	ReasonNamesNone:     {"The rules have no tailnet short names yet, so short names go through the proxy.", "Regenerate the Tailscale coexistence override and apply it: it writes rules for the current devices."},
	ReasonNamesOutdated: {"The device short names in the rules do not match the tailnet now.", "Regenerate the Tailscale coexistence override and apply it: it updates the rules to the current devices."},

	ReasonBindStale:   {"Tailnet name lookups are bound to Tailscale's old adapter and time out.", "Regenerate the Tailscale coexistence override and apply it: it binds them to Tailscale's adapter now."},
	ReasonBindMissing: {"Tailnet name lookups leave through the physical adapter and never reach Tailscale.", "Regenerate the Tailscale coexistence override and apply it: it binds them to Tailscale's adapter and writes hosts entries."},

	ReasonFakeIPWildcard: {"The fake-ip filter mode is not blacklist, so single-label names get fake addresses.", "In the override that sets fake-ip-filter-mode, set it back to blacklist (or remove the line), then save and apply."},

	ReasonDNSNotMihomo:              {"Some network connections do not use mihomo for DNS, so routing and leak protection miss them.", "Point their DNS at mihomo. The old settings are backed up first; you can undo."},
	ReasonDNSViaHijack:              {"Some network connections do not use mihomo for DNS; the TUN's hijack catches them for now.", "Point their DNS at mihomo directly. The old settings are backed up first; you can undo."},
	ReasonDNSNotMihomo + "@" + Linux: {"Some network connections do not use mihomo for DNS, so routing and leak protection miss them.", "On Linux Twinnel cannot set the system DNS for you yet. In network settings set this connection's DNS to 127.0.0.1 with automatic DNS off, or use nmcli (see details)."},
	ReasonDNSViaHijack + "@" + Linux: {"Some network connections do not use mihomo for DNS; the TUN's hijack catches them for now.", "On Linux Twinnel cannot set the system DNS for you yet. In network settings set this connection's DNS to 127.0.0.1 with automatic DNS off, or use nmcli (see details)."},

	ReasonVPNOther:                    {"Another VPN is connected and competes with mihomo for the same route.", "That VPN is another app you installed; Twinnel does not disconnect it. Disconnect it in that app, or in System Settings → VPN."},
	ReasonVPNOther + "@" + Windows:    {"Another VPN is connected and competes with mihomo for the same route.", "That VPN is another app you installed; Twinnel does not disconnect it. Disconnect it in that app, or in Settings → Network & internet → VPN."},
	ReasonVPNOther + "@" + Linux:      {"Another VPN is connected and competes with mihomo for the same route.", "That VPN is another app you installed; Twinnel does not disconnect it. Disconnect it in that app or in network settings (nmcli connection down <name>)."},
	ReasonVPNTailscale:                {"The App Store Tailscale is installed and holds the system's VPN slot.", "macOS manages that version, so Twinnel cannot change it. Quit it and use the open-source tailscaled (Twinnel's setup can install it), or accept the conflict."},
	ReasonVPNExitNode:                 {"Tailscale uses an exit node; it and mihomo's TUN both want all traffic.", "Stop using the exit node. You can pick it again on the Tailscale page."},
	ReasonVPNOtherTUN:                 {"Another tunnel holds the default route; traffic does not go through mihomo.", "That tunnel belongs to another VPN app; Twinnel does not close it. Disconnect it in that app, or in System Settings → VPN."},
	ReasonVPNOtherTUN + "@" + Windows: {"Another tunnel holds the default route; traffic does not go through mihomo.", "That tunnel belongs to another VPN app; Twinnel does not close it. Disconnect it in that app, or in Settings → Network & internet → VPN."},
	ReasonVPNOtherTUN + "@" + Linux:   {"Another tunnel holds the default route; traffic does not go through mihomo.", "That tunnel belongs to another VPN app; Twinnel does not close it. Disconnect it in that app or in network settings (nmcli connection down <name>, wg-quick down <interface>)."},
	ReasonVPNNotDefault:               {"mihomo's TUN is on but does not hold the default route, so traffic skips it.", "Restart mihomo so it takes the default route again. The network drops for a few seconds."},

	ReasonLeakMode:     {"mihomo is not in rule mode, so the leak-guarded groups do not apply.", "Switch mihomo back to rule mode."},
	ReasonLeakNoGroup:  {"A group to protect does not exist in the current config.", "In Leak guard, choose the groups your config really has."},
	ReasonLeakSelected: {"A sensitive group leaves through this computer's own network now.", "Turn on the leak guard override and apply it: DIRECT is removed from these groups; with no node left they block instead."},
	ReasonLeakMayGo:    {"A sensitive group may switch to DIRECT and leave through this computer's own network.", "Turn on the leak guard override and apply it: DIRECT is removed from these groups; with no node left they block instead."},

	ReasonEgressFailed: {"The test request through mihomo failed, so the egress was not measured.", "Test again. If it still fails the node is likely down: pick another and test again."},
	ReasonEgressSame:   {"The proxied IP is this computer's own: the node does not really proxy.", "Pick another node for this group on the Proxies page, then test the egress again."},

	ReasonCtlExposed:  {"mihomo's controller is open to the local network; other devices may control it.", "Add an override that binds the controller to this computer (127.0.0.1) and apply it. You can undo."},
	ReasonCtlNoSecret: {"mihomo's controller has no secret: any program on this computer can control it.", "Generate a random secret and apply it; the helper keeps it. Other tools such as a web dashboard need the new secret."},
}

var fixTextTables = map[string]map[string]FixText{"zh-Hans": zhFixTexts, "en": enFixTexts}

// FixTextFor returns the texts of reason on platform in a language tag.
func FixTextFor(tag, platform, reason string) (FixText, bool) {
	tbl := fixTextTables[LangOf(WithLang(context.Background(), tag))]
	if t, ok := tbl[reason+"@"+platform]; ok {
		return t, true
	}
	t, ok := tbl[reason]
	return t, ok
}
