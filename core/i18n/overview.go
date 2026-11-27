package i18n

// Texts of the 总览 page (cmd/app overview.go): its quick actions, glance
// tiles and cards.

// OverviewStrings holds them.
type OverviewStrings struct {
	OvCopyProxyEnv  string // quick action: copy `export https_proxy=…`
	OvDashboard     string // quick action: open the external UI in the browser
	OvRestartTitle  string // confirm before restarting mihomo
	OvRestartBody   string
	OvTileExit      string // tile: the exit: its delay, node and public IP
	OvEgressCheck   string // tile: run the egress test to learn the exit IP
	OvEgressVia     string // health row: the public IP through mihomo
	OvEgressDirect  string // health row: the public IP without mihomo
	OvTileMemory    string // tile: mihomo's memory
	OvTileTotal     string // %s %s: ↓ total, ↑ total
	OvCPU           string // %s: "CPU 1.8%"
	OvUptime        string // row label: how long a daemon has run
	OvProcess       string // row label: memory · CPU · uptime
	OvDNS           string
	OvDNSOff        string
	OvDNSNormal     string
	OvProxyPort     string
	OvThisDevice    string
	OvTailnet       string
	OvDevices       string // Tailscale menu: open the devices page
	OvAdminConsole  string // Tailscale menu: the admin console
	OvTakeover      string // duet card: takeover row
	OvTakenOver     string
	OvNotTakenOver  string
	OvNeverApplied  string
	OvApplyFailed   string // after the time of a failed apply
	OvHealth        string // duet card: health row
	OvHealthRerun   string // duet card's trailing button
	OvHealthOK      string
	OvHealthFail    string // %d
	OvHealthIssues  string // %d: findings rated a warning
	OvRecent        string // card title: the last duet events
	OvRecentAll     string // its trailing button: 日志 → duet
	OvHelperVersion string // %s
	OvMemoryTip     string // %s %s: what mihomo reports, what the system reports
}

var zhOverview = OverviewStrings{
	OvCopyProxyEnv:  "复制终端代理",
	OvDashboard:     "控制面板",
	OvRestartTitle:  "重启 mihomo？",
	OvRestartBody:   "现有连接会断开，几秒内没有网络。",
	OvTileExit:      "出口",
	OvEgressCheck:   "检测出口 IP",
	OvEgressVia:     "经 mihomo",
	OvEgressDirect:  "直连",
	OvTileMemory:    "mihomo 内存",
	OvTileTotal:     "↓ %s ↑ %s",
	OvCPU:           "CPU %s",
	OvUptime:        "运行时长",
	OvProcess:       "进程",
	OvDNS:           "DNS",
	OvDNSOff:        "未启用",
	OvDNSNormal:     "普通",
	OvProxyPort:     "代理端口",
	OvThisDevice:    "本机",
	OvTailnet:       "Tailnet",
	OvDevices:       "查看设备",
	OvAdminConsole:  "打开管理控制台",
	OvTakeover:      "接管",
	OvTakenOver:     "已接管",
	OvNotTakenOver:  "未接管",
	OvNeverApplied:  "还没有应用过",
	OvApplyFailed:   "失败",
	OvHealth:        "健康检查",
	OvHealthRerun:   "重新检查",
	OvHealthOK:      "全部通过",
	OvHealthFail:    "%d 项失败",
	OvHealthIssues:  "%d 个问题",
	OvRecent:        "最近活动",
	OvRecentAll:     "全部日志",
	OvHelperVersion: "v%s",
	OvMemoryTip:     "mihomo 自报 %s · 系统统计 %s",
}

var enOverview = OverviewStrings{
	OvCopyProxyEnv:  "Copy Proxy Env",
	OvDashboard:     "Dashboard",
	OvRestartTitle:  "Restart mihomo?",
	OvRestartBody:   "Open connections drop and the network is down for a few seconds.",
	OvTileExit:      "Exit",
	OvEgressCheck:   "Check Exit IP",
	OvEgressVia:     "Via mihomo",
	OvEgressDirect:  "Direct",
	OvTileMemory:    "mihomo memory",
	OvTileTotal:     "↓ %s ↑ %s",
	OvCPU:           "CPU %s",
	OvUptime:        "Uptime",
	OvProcess:       "Process",
	OvDNS:           "DNS",
	OvDNSOff:        "Off",
	OvDNSNormal:     "Normal",
	OvProxyPort:     "Proxy port",
	OvThisDevice:    "This Mac",
	OvTailnet:       "Tailnet",
	OvDevices:       "Show Devices",
	OvAdminConsole:  "Open Admin Console",
	OvTakeover:      "Takeover",
	OvTakenOver:     "Managed by duet",
	OvNotTakenOver:  "Not taken over",
	OvNeverApplied:  "Not applied yet",
	OvApplyFailed:   "failed",
	OvHealth:        "Health",
	OvHealthRerun:   "Check Again",
	OvHealthOK:      "All passed",
	OvHealthFail:    "%d failed",
	OvHealthIssues:  "%d issue{s}",
	OvRecent:        "Recent activity",
	OvRecentAll:     "All Logs",
	OvHelperVersion: "v%s",
	OvMemoryTip:     "mihomo reports %s · the system counts %s",
}
