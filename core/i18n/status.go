package i18n

// Texts of the status and control surfaces (overview, flyout, sidebar
// foot, health page) added with the second UX pass: the capability lock,
// first-launch findings, the accepted exit node, recovery actions, fix
// buttons. Embedded in Strings; fields are promoted (T.NeedsHelper).

// StatusStrings holds them.
type StatusStrings struct {
	// A control that needs the helper: the lock's tooltip.
	NeedsHelper string

	// Before takeover, health failures are findings, not faults.
	ReasonHealthFound string // %d: count
	FindingsView      string // → health page
	FindingsTakeover  string // → takeover page

	// 开始使用: three steps until Twinnel runs the show.
	GetStarted, StepCheckCoexist, StepTakeOver string

	// The accepted exit node.
	ExitNodeAcceptedRow string // %s: exit node
	ExitNodeAccept      string
	ExitNodeTurnOff     string
	HealthAccepted      string // a check's state
	CausedByExitNode    string // the group of checks it breaks

	// Health report age and evidence.
	HealthAge     string // %s: "3 分钟前"
	EvidenceTimes string // %d: how often a line repeats
	EvidenceAll   string // %d: lines

	// Fix buttons (health.Fix* codes).
	FixRegenCoexist, FixOpenOverrides, FixTakeOver, FixInstallHelper, FixTestEgress string
	// 出口 IP 实测: test again, the time of an on-demand result (%s), the
	// 定期检测 switch.
	EgressRetest, EgressTestedAt, EgressPeriodic, EgressUntested, EgressFailed string
	DoneCoexistRegen                                                           string

	// mihomo down: start it through the helper.
	StartMihomo, RestartMihomo, StartShort string

	// Tailscale logged out.
	TSLogIn string

	// The helper in the flyout and the sidebar foot.
	HelperDownRow string
	HelperShort   string

	// 应急恢复: helper-backed actions that work while things are broken.
	Recovery, RecoveryConnectivity string

	// Key expiry within the warning window.
	KeyRenew string

	// The tray's tooltip: app · level · first reason.
	TrayTipSep string
}

var zhStatus = StatusStrings{
	NeedsHelper: "需要后台助手",

	ReasonHealthFound: "发现 %d 个共存问题",
	FindingsView:      "查看",
	FindingsTakeover:  "接管后可自动处理",

	GetStarted:       "开始使用",
	StepCheckCoexist: "检查共存",
	StepTakeOver:     "接管现有配置",

	ExitNodeAcceptedRow: "出口节点 %s · 已接受与代理冲突",
	ExitNodeAccept:      "接受",
	ExitNodeTurnOff:     "关闭出口节点",
	HealthAccepted:      "已接受",
	CausedByExitNode:    "由出口节点导致",

	HealthAge:     "%s更新",
	EvidenceTimes: "×%d",
	EvidenceAll:   "显示全部（%d 行）",

	FixRegenCoexist:  "重新生成 Tailscale 共存",
	FixOpenOverrides: "打开订阅与覆写",
	FixTakeOver:      "接管",
	FixInstallHelper: "安装后台助手",
	FixTestEgress:    "检测出口",
	EgressRetest:     "重新检测",
	EgressTestedAt:   "%s 检测",
	EgressPeriodic:   "定期检测",
	EgressUntested:   "未测过",
	EgressFailed:     "没能检测出口",
	DoneCoexistRegen: "已重新生成 Tailscale 共存",

	StartMihomo:   "启动 mihomo",
	RestartMihomo: "重启 mihomo",
	StartShort:    "启动",

	TSLogIn: "登录…",

	HelperDownRow: "后台助手未运行",
	HelperShort:   "助手",

	Recovery:             "应急恢复",
	RecoveryConnectivity: "连通性测试",

	KeyRenew: "在管理后台续期",

	TrayTipSep: " · ",
}

var enStatus = StatusStrings{
	NeedsHelper: "Needs the helper",

	ReasonHealthFound: "%d coexistence issue{s} found",
	FindingsView:      "View",
	FindingsTakeover:  "Fix by Taking Over",

	GetStarted:       "Get started",
	StepCheckCoexist: "Check coexistence",
	StepTakeOver:     "Take over your setup",

	ExitNodeAcceptedRow: "Exit node %s · proxy conflict accepted",
	ExitNodeAccept:      "Accept",
	ExitNodeTurnOff:     "Turn Off Exit Node",
	HealthAccepted:      "Accepted",
	CausedByExitNode:    "Caused by the exit node",

	HealthAge:     "Updated %s",
	EvidenceTimes: "×%d",
	EvidenceAll:   "Show All (%d line{s})",

	FixRegenCoexist:  "Regenerate Tailscale Coexistence",
	FixOpenOverrides: "Open Subscriptions & Overrides",
	FixTakeOver:      "Take Over",
	FixInstallHelper: "Install Helper",
	FixTestEgress:    "Test Egress",
	EgressRetest:     "Test Again",
	EgressTestedAt:   "Tested %s",
	EgressPeriodic:   "Test Periodically",
	EgressUntested:   "Not tested yet",
	EgressFailed:     "Couldn't test the egress",
	DoneCoexistRegen: "Tailscale coexistence regenerated",

	StartMihomo:   "Start mihomo",
	RestartMihomo: "Restart mihomo",
	StartShort:    "Start",

	TSLogIn: "Log In…",

	HelperDownRow: "Helper not running",
	HelperShort:   "Helper",

	Recovery:             "Recovery",
	RecoveryConnectivity: "Test Connectivity",

	KeyRenew: "Renew in Admin Console",

	TrayTipSep: " · ",
}
