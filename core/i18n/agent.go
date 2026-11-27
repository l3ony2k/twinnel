package i18n

// Texts of 设置 → 本地 Agent (cmd/app/agentsection.go): the MCP switches,
// the command line tool, the client snippets and recent agent activity,
// and the agent rows of 日志 → duet. Embedded in Strings; fields are
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
	AgentCLIForeign   string // %s: a path that is not duet's link

	// Client snippets.
	AgentConnect     string // the row label
	AgentCopy        string
	AgentClientOther string // "Other (JSON)"

	// Recent activity.
	AgentActivity     string
	AgentActivityNone string
	AgentActivityMore string // %d more{s}
	AgentLoadFailed   string // failedRow: reading the switches / activity

	// 日志 → duet.
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

	AgentConnect:     "连接",
	AgentCopy:        "复制",
	AgentClientOther: "其他 (JSON)",

	AgentActivity:     "最近操作",
	AgentActivityNone: "还没有 Agent 操作",
	AgentActivityMore: "另有 %d 条",
	AgentLoadFailed:   "没能读取 Agent 设置",

	EvAgent:       "%s · %s",
	EvAgentFailed: "%s · %s 失败",

	TipAgent:          "让本机的 AI Agent（Claude Code、Codex、Cursor 等）通过 MCP 查看和操作 duet。每次改动都记在 日志 → duet，并标明是哪个 Agent。",
	TipAgentMCP:       "关闭时，Agent 什么都读不到，也改不了。打开后，查看状态、健康检查、日志等只读工具始终可用。",
	TipAgentOperate:   "可以随时撤销的操作：切换模式和节点、测速、开关 TUN、连接或断开 Tailscale、出口节点、更新订阅。",
	TipAgentConfigure: "修改覆写和订阅、应用配置、回滚历史、改设置。应用前 Agent 必须先拿到预览并交给你确认。安装、卸载、接管和内核更新只能在 duet 里做。",
	TipAgentCLI:       "在终端里用 duet 命令，Agent 的 MCP 配置也指向它。安装是建一个指向 duet.app 的链接，随时可以移除。",
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

	AgentConnect:     "Connect",
	AgentCopy:        "Copy",
	AgentClientOther: "Other (JSON)",

	AgentActivity:     "Recent actions",
	AgentActivityNone: "No agent actions yet",
	AgentActivityMore: "%d more",
	AgentLoadFailed:   "Couldn't read the agent settings",

	EvAgent:       "%s · %s",
	EvAgentFailed: "%s · %s failed",

	TipAgent:          "Lets AI agents on this Mac (Claude Code, Codex, Cursor and others) see and operate duet through MCP. Every change shows in Logs → duet with the agent's name.",
	TipAgentMCP:       "Off: agents can neither read nor change anything. On: read-only tools such as status, health checks and logs are always available.",
	TipAgentOperate:   "Changes you can undo at any time: mode and node, delay tests, TUN, connecting Tailscale, exit node, refreshing subscriptions.",
	TipAgentConfigure: "Edit overrides and subscriptions, apply, roll back, change settings. Before applying, an agent must get the preview and show it to you. Install, uninstall, takeover and core updates stay in duet.",
	TipAgentCLI:       "Use duet in Terminal; the agents' MCP settings point to it too. Installing makes a link to duet.app that you can remove at any time.",
}
