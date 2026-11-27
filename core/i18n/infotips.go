package i18n

// The ⓘ popovers (cmd/app/infotip.go) outside the 接管 page and the menu
// bar panel: what a control or section is, what it changes, when to use
// it. One to three short sentences each. Embedded in Strings; fields are
// promoted (T.TipMagicDNS).

// InfoTipStrings holds them.
type InfoTipStrings struct {
	// The ⓘ button's name for assistive technology: InfoTipLabel alone,
	// or InfoTipAbout with the thing it explains.
	InfoTipLabel, InfoTipAbout string

	TipExitAccepted                  string // 总览: the accepted exit node row
	TipMagicDNS, TipPeerPath         string // Tailscale 设备
	TipHealthAccepted                string // 健康检查 → 已接受
	TipEgress                        string // 出口 IP 实测: what 检测出口 sends; 定期检测
	TipHistRollback                  string // 历史 → 这条记录
	TipKernelState, TipGeo           string // 内核
	TipLogs                          string // 日志: the three sources and the level
	TipPrimaryGroup                  string // 设置 → mihomo
	TipNotifications, TipOpenAtLogin string // 设置
	TipHelper                        string // 设置 → 后台助手
	TipInstallPlan                   string // 安装后台助手 → 安装时会做什么

	// 停止使用 Twinnel: what each way out does and leaves.
	TipLeaveUndo, TipLeaveUninstall, TipLeaveRemoveAll, TipLeaveExport string
}

var zhInfoTips = InfoTipStrings{
	InfoTipLabel: "说明",
	InfoTipAbout: "关于%s",

	TipExitAccepted: "你确认过在 TUN 开着时使用这个出口节点。被它影响的健康检查不再算作问题，停用出口节点后恢复。",
	TipMagicDNS:     "用设备名访问 Tailscale 里的设备。mihomo 接管 DNS 时，Twinnel 的共存设置把这些名字交给 Tailscale 解析。",
	TipPeerPath:     "直连：两台设备点对点相连。\nDERP 中继：流量经 Tailscale 的中继服务器转发，通常更慢；DERP 一列是所用的中继区域。",

	TipHealthAccepted: "你接受的情况，例如 TUN 开着时使用出口节点。它们照常检查，但不再算作问题。",
	TipEgress:         "检测出口直连和经 mihomo 各向 IP 回显服务（1.1.1.1）发一次 HTTPS 请求，比较两边的出口 IP，看敏感策略组是否从本机的网络出去。开启定期检测后每次健康检查都测；关闭时只在你点「检测出口」时测，结果保留 24 小时。",
	TipHistRollback:   "回滚把这条记录的配置和当时的覆写设为当前配置，并重载 mihomo；现在的配置也会留在历史里。「接管前」是接管时备份的原配置。",

	TipKernelState: "Twinnel 管理：Twinnel 下载并校验 mihomo，可以更新和回滚。\n外部程序：服务仍在用你自己的程序，Twinnel 不更新它。\n待重启：新版本已装好，重启 mihomo 后生效。",
	TipGeo:         "mihomo 规则里 GEOIP、GEOSITE 用到的数据文件，按配置里的 geox-url 下载。「可校验」的文件下载后会核对校验值。",

	TipLogs: "mihomo：内核的实时日志，从打开页面起记录。\nTwinnel：后台助手做过的操作。\n后台助手：它自己的运行日志，排查问题时用。\n级别只影响这里显示什么，不改 mihomo 的配置。",

	TipPrimaryGroup:  "Twinnel 把这个策略组当作出口：总览和菜单栏显示它选的节点和延迟。",
	TipNotifications: "mihomo 停止响应、Tailscale 需要登录或密钥快过期、健康检查失败时通知你。",
	TipOpenAtLogin:   "登录后 Twinnel 出现在菜单栏。mihomo 和 Tailscale 由系统服务运行，不需要 Twinnel 开着。",
	TipHelper:        "以 root 运行的系统服务。改配置、更新订阅和内核、启停 mihomo 服务都由它完成，Twinnel 本身不需要管理员权限。",
	TipInstallPlan:   "这些步骤只在安装时以管理员权限运行一次。之后随时可以在 设置 → 停止使用 Twinnel 卸载。",

	TipLeaveUndo:      "写回接管前的 config.yaml，移除导入的订阅和覆写。mihomo 的服务和程序不变，后台助手保留。",
	TipLeaveUninstall: "移除后台助手。mihomo 和 Tailscale 继续运行，mihomo 用现在的配置。",
	TipLeaveRemoveAll: "卸载后台助手，把 Twinnel.app 移到废纸篓。mihomo 和 Tailscale 继续运行。不会撤销接管：要换回原来的配置，先撤销接管。",
	TipLeaveExport:    "把当前运行的 config.yaml、订阅和你写的覆写打包成 zip：停用 Twinnel 后也能直接给 mihomo 用，也能在另一台电脑的「订阅与覆写 → 导入…」里导入。里面有订阅链接和节点信息，注意保管。",
}

var enInfoTips = InfoTipStrings{
	InfoTipLabel: "More info",
	InfoTipAbout: "About %s",

	TipExitAccepted: "You chose to use this exit node while TUN is on. Health checks it affects no longer count as problems until you stop using the exit node.",
	TipMagicDNS:     "Reach tailnet devices by name. While mihomo handles DNS, Twinnel's coexistence settings send these names to Tailscale.",
	TipPeerPath:     "Direct: the two devices connect peer to peer.\nDERP relay: traffic goes through Tailscale's relay servers, usually slower; the DERP column is the relay region.",

	TipHealthAccepted: "Situations you accepted, such as using an exit node while TUN is on. They are still checked but no longer count as problems.",
	TipEgress:         "Test Egress sends one HTTPS request directly and one through mihomo to an IP echo service (1.1.1.1) and compares the two public IPs, to see whether the sensitive groups leave from this network. With Test Periodically on, every health check tests it; when off, it runs only when you click Test Egress, and the result is kept for 24 hours.",
	TipHistRollback:   "Rolling back makes this entry's config, with the overrides it had, the current config and reloads mihomo. The config you have now stays in history. “Before Twinnel” is the original config backed up at takeover.",

	TipKernelState: "Managed by Twinnel: Twinnel downloads and verifies mihomo and can update or roll it back.\nExternal binary: the service still runs your own binary, which Twinnel doesn't update.\nRestart pending: a new version is installed and takes effect when mihomo restarts.",
	TipGeo:         "The data files that GEOIP and GEOSITE rules use, downloaded from the config's geox-url. Files marked Verifiable are checked against a published digest.",

	TipLogs: "mihomo shows the core's live log from when you open this page.\nTwinnel shows what the helper has done.\nHelper shows the helper's own log, for troubleshooting.\nThe level only filters what shows here; mihomo's config is not changed.",

	TipPrimaryGroup:  "Twinnel treats this group as the exit: the overview and the menu bar show the node it chose and its latency.",
	TipNotifications: "You get a notification when mihomo stops responding, Tailscale needs a login or its key is about to expire, or a health check fails.",
	TipOpenAtLogin:   "Twinnel appears in the menu bar after you log in. mihomo and Tailscale run as system services and don't need Twinnel open.",
	TipHelper:        "A system service that runs as root. It changes the config, updates subscriptions and the core, and starts or stops mihomo's service, so Twinnel itself never needs administrator rights.",
	TipInstallPlan:   "These steps run once, with administrator rights, at install. You can uninstall at any time in Settings → Stop Using Twinnel.",

	TipLeaveUndo:      "Writes back the config.yaml from before the takeover and removes the imported subscription and overrides. mihomo's service and binary stay, and so does the helper.",
	TipLeaveUninstall: "Removes the helper. mihomo and Tailscale keep running, mihomo with the current config.",
	TipLeaveRemoveAll: "Uninstalls the helper and moves Twinnel.app to the Trash. mihomo and Tailscale keep running. It doesn't undo the takeover: to get your original config back, undo the takeover first.",
	TipLeaveExport:    "Saves the running config.yaml, your subscriptions and the overrides you wrote as a zip: mihomo can use it without Twinnel, and another computer can import it in Subscriptions & Overrides → Import…. It contains subscription links and your nodes, so keep it private.",
}
