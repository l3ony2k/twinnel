package i18n

// Texts of 设置 → 本地 Agent (cmd/app/agentsection.go): the MCP switches,
// the command line tool, the client snippets and recent agent activity,
// and the agent rows of 日志 → Twinnel. Embedded in Strings; fields are
// promoted (T.AgentSection).

// AgentStrings holds them.
type AgentStrings struct {
	AgentSection string

	// The switches.
	AgentMCP, AgentOperate, AgentConfigure string
	AgentNeedsHelper                       string // why the switches are off

	// 命令行工具.
	AgentCLI          string
	AgentCLIInstall   string // the button
	AgentCLIRemove    string
	AgentCLINone      string // not installed
	AgentCLIInstalled string // %s: the path (toast)
	AgentCLIRemoved   string // toast
	AgentCLIFailed    string // failedRow: what did not work
	AgentCLIForeign   string // %s: a path that is not Twinnel's link
	AgentCLIOnPath    string // %s: the folder put on PATH (toast, Windows)

	// Client snippets.
	AgentConnect     string // the row label
	AgentCopy        string
	AgentClientOther string // "Other (JSON)"

	// Recent activity.
	AgentActivity     string
	AgentActivityNone string
	AgentActivityMore string // %d more{s}
	AgentLoadFailed   string // failedRow: reading the switches / activity

	// An action in words, per MCP tool (core/agent): AgentTools without
	// an object, AgentToolsOf with one (%s: an override, a subscription,
	// a node, a mode). A tool that turns something on or off has a
	// "tool:on" and "tool:off" entry instead. Unknown tools show as they
	// are; the raw arguments stay in the tooltip.
	AgentTools, AgentToolsOf map[string]string

	// 日志 → Twinnel.
	EvAgent       string // %s: client, %s: what it did
	EvAgentFailed string // %s: client, %s: what it tried

	// ⓘ.
	TipAgent, TipAgentMCP, TipAgentOperate, TipAgentConfigure, TipAgentCLI string
}

var zhAgent = AgentStrings{
	AgentSection: "本地 Agent",

	AgentMCP:         "允许 MCP",
	AgentOperate:     "操作",
	AgentConfigure:   "修改配置",
	AgentNeedsHelper: "需要后台助手",

	AgentCLI:          "命令行工具",
	AgentCLIInstall:   "安装",
	AgentCLIRemove:    "移除",
	AgentCLINone:      "未安装",
	AgentCLIInstalled: "已安装到 %s",
	AgentCLIRemoved:   "已移除命令行工具",
	AgentCLIFailed:    "没能安装命令行工具",
	AgentCLIForeign:   "%s 已被其他程序占用",
	AgentCLIOnPath:    "已把 %s 加入 PATH，新打开的终端可以用 twinnel",

	AgentConnect:     "连接",
	AgentCopy:        "复制",
	AgentClientOther: "其他 (JSON)",

	AgentActivity:     "最近操作",
	AgentActivityNone: "还没有 Agent 操作",
	AgentActivityMore: "另有 %d 条",
	AgentLoadFailed:   "没能读取 Agent 设置",

	AgentTools: map[string]string{
		"apply": "应用配置", "history_rollback": "回滚配置", "settings_update": "修改设置", "config_import": "导入配置",
		"layer_put": "保存覆写", "layer_remove": "删除覆写", "layer_enable:on": "启用覆写", "layer_enable:off": "停用覆写", "layer_order": "调整覆写顺序",
		"sub_put": "保存订阅", "sub_activate": "切换订阅", "sub_remove": "删除订阅", "refresh_subscription": "更新订阅",
		"set_mode": "切换模式", "select_node": "选择节点", "health_egress_run": "检测出口", "delay_test": "测速",
		"set_tun:on": "打开 TUN", "set_tun:off": "关闭 TUN", "tailscale_up": "连接 Tailscale", "tailscale_down": "断开 Tailscale",
		"set_exit_node": "设置出口节点", "clear_exit_node": "停用出口节点", "geo_update": "更新 GEO 数据库",
		"dashboard_install": "安装控制面板", "flush_fakeip": "清空 fake-ip 缓存",
		"health_fix": "修复健康检查", "health_fix_config": "修复健康检查", "health_fixes": "查看修复建议",
		"status": "查看状态", "version": "查看版本", "health": "查看健康检查", "proxies": "查看策略组", "connections": "查看连接",
		"traffic": "查看流量", "logs": "查看日志", "subscriptions": "查看订阅", "layers": "查看覆写", "history": "查看历史",
		"preview": "预览配置", "takeover_plans": "查看接管计划", "tailnet_peers": "查看 Tailscale 设备", "doctor": "诊断",
		"agent_activity": "查看 Agent 操作", "admin_only": "查看需要你来做的操作",
	},
	AgentToolsOf: map[string]string{
		"layer_put": "保存覆写「%s」", "layer_remove": "删除覆写「%s」", "layer_enable:on": "启用覆写「%s」", "layer_enable:off": "停用覆写「%s」",
		"sub_put": "保存订阅「%s」", "sub_activate": "切换到订阅「%s」", "sub_remove": "删除订阅「%s」", "refresh_subscription": "更新订阅「%s」",
		"set_mode": "切换到 %s 模式", "select_node": "选择节点 %s", "set_exit_node": "把出口节点设为 %s",
	},

	EvAgent:       "%s · %s",
	EvAgentFailed: "%s · %s 失败",

	TipAgent:          "让本机的 AI Agent（Claude Code、Codex、Cursor 等）通过 MCP 查看和操作 Twinnel。每次改动都记在 日志 → Twinnel，并标明是哪个 Agent。",
	TipAgentMCP:       "关闭时，Agent 什么都读不到，也改不了。打开后，查看状态、健康检查、日志等只读工具始终可用。",
	TipAgentOperate:   "可以随时撤销的操作：切换模式和节点、测速、开关 TUN、连接或断开 Tailscale、出口节点、更新订阅。",
	TipAgentConfigure: "修改覆写和订阅、应用配置、回滚历史、改设置。应用前 Agent 必须先拿到预览并交给你确认。安装、卸载、接管和内核更新只能在 Twinnel 里做。",
	TipAgentCLI:       "在终端里用 Twinnel 命令，Agent 的 MCP 配置也指向它。安装是建一个指向 Twinnel.app 的链接，随时可以移除。",
}

var enAgent = AgentStrings{
	AgentSection: "Local agents",

	AgentMCP:         "Allow MCP",
	AgentOperate:     "Operate",
	AgentConfigure:   "Change configuration",
	AgentNeedsHelper: "Needs the helper",

	AgentCLI:          "Command line tool",
	AgentCLIInstall:   "Install",
	AgentCLIRemove:    "Remove",
	AgentCLINone:      "Not installed",
	AgentCLIInstalled: "Installed in %s",
	AgentCLIRemoved:   "Command line tool removed",
	AgentCLIFailed:    "Couldn't install the command line tool",
	AgentCLIForeign:   "%s is taken by another program",
	AgentCLIOnPath:    "Added %s to PATH; new terminals can run twinnel",

	AgentConnect:     "Connect",
	AgentCopy:        "Copy",
	AgentClientOther: "Other (JSON)",

	AgentActivity:     "Recent actions",
	AgentActivityNone: "No agent actions yet",
	AgentActivityMore: "%d more",
	AgentLoadFailed:   "Couldn't read the agent settings",

	AgentTools: map[string]string{
		"apply": "Apply the config", "history_rollback": "Roll back the config", "settings_update": "Change settings", "config_import": "Import a config",
		"layer_put": "Save an override", "layer_remove": "Remove an override", "layer_enable:on": "Turn on an override", "layer_enable:off": "Turn off an override", "layer_order": "Reorder overrides",
		"sub_put": "Save a subscription", "sub_activate": "Switch subscriptions", "sub_remove": "Remove a subscription", "refresh_subscription": "Update a subscription",
		"set_mode": "Change the mode", "select_node": "Choose a node", "health_egress_run": "Test the exit IP", "delay_test": "Test delays",
		"set_tun:on": "Turn on TUN", "set_tun:off": "Turn off TUN", "tailscale_up": "Connect Tailscale", "tailscale_down": "Disconnect Tailscale",
		"set_exit_node": "Set the exit node", "clear_exit_node": "Stop using the exit node", "geo_update": "Update GEO databases",
		"dashboard_install": "Install the dashboard", "flush_fakeip": "Clear the fake-ip cache",
		"health_fix": "Fix a health check", "health_fix_config": "Fix a health check", "health_fixes": "Read health fixes",
		"status": "Read status", "version": "Read versions", "health": "Read health checks", "proxies": "Read proxy groups", "connections": "Read connections",
		"traffic": "Read traffic", "logs": "Read logs", "subscriptions": "Read subscriptions", "layers": "Read overrides", "history": "Read history",
		"preview": "Preview the config", "takeover_plans": "Read takeover plans", "tailnet_peers": "Read Tailscale devices", "doctor": "Run the doctor",
		"agent_activity": "Read agent actions", "admin_only": "Read actions left to you",
	},
	AgentToolsOf: map[string]string{
		"layer_put": "Save override “%s”", "layer_remove": "Remove override “%s”", "layer_enable:on": "Turn on override “%s”", "layer_enable:off": "Turn off override “%s”",
		"sub_put": "Save subscription “%s”", "sub_activate": "Switch to subscription “%s”", "sub_remove": "Remove subscription “%s”", "refresh_subscription": "Update subscription “%s”",
		"set_mode": "Switch to %s mode", "select_node": "Choose node %s", "set_exit_node": "Set the exit node to %s",
	},

	EvAgent:       "%s · %s",
	EvAgentFailed: "%s · %s failed",

	TipAgent:          "Lets AI agents on this Mac (Claude Code, Codex, Cursor and others) see and operate Twinnel through MCP. Every change shows in Logs → Twinnel with the agent's name.",
	TipAgentMCP:       "Off: agents can neither read nor change anything. On: read-only tools such as status, health checks and logs are always available.",
	TipAgentOperate:   "Changes you can undo at any time: mode and node, delay tests, TUN, connecting Tailscale, exit node, refreshing subscriptions.",
	TipAgentConfigure: "Edit overrides and subscriptions, apply, roll back, change settings. Before applying, an agent must get the preview and show it to you. Install, uninstall, takeover and core updates stay in Twinnel.",
	TipAgentCLI:       "Use Twinnel in Terminal; the agents' MCP settings point to it too. Installing makes a link to Twinnel.app that you can remove at any time.",
}
