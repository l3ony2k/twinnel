// Package i18n holds every UI string of the app, one Strings table per
// language (lang.go lists them). Chinese, below, is the reference; en.go
// is English. Add a language by writing its table and listing it in
// langs.
package i18n

import "fmt"

// Strings is the full set of UI texts. Fields, not map keys, so that a
// missing text is a compile error.
type Strings struct {
	AppName string

	// Overall status.
	StatusOK, StatusWarn, StatusError, StatusUnknown string

	// Reasons for a status that is not OK.
	ReasonMihomoDown     string // the reason in words follows (NetReason)
	ReasonMihomoAuth     string
	ReasonTunOff         string
	ReasonPrimaryDead    string // %s: group
	ReasonTSDown         string // the reason in words follows (NetReason)
	ReasonTSNotInstalled string
	ReasonTSNeedsLogin   string
	ReasonTSStopped      string
	ReasonTSExitNode     string // %s: exit node name
	ReasonTSKeyExpiry    string // %s: duration
	ReasonTSHealth       string // %s: message
	ReasonTSSystemVPN    string
	ReasonHealthWarn     string // %d: count
	ReasonHealthFail     string // %d: count

	// Desktop notifications.
	AlertMihomoDown, AlertTSNeedsLogin, AlertTSKeyExpiry string
	AlertTSKeyExpiryBody                                 string // %s: date, %s: in how long
	AlertHealthFail                                      string // %s: check title

	// Sections and labels.
	Mihomo, Tailscale                                          string
	Running, NotRunning, Unreachable                           string
	Mode, ModeRule, ModeGlobal, ModeDirect                     string
	Tun, TunOn, TunOff                                         string
	Version                                                    string
	PrimaryGroup, Node, Delay, Exit                            string
	DelayTimeout, DelayUnknown                                 string
	Traffic, Upload, Download, Total                           string
	TrafficLastSec, TrafficLastMin                             string // %d: seconds / minutes shown by the chart
	Connections                                                string
	State, SelfIP, SelfName, OnlinePeers, ExitNode, NoExitNode string
	MagicDNS, ShieldsUp, KeyExpiry, Account, Install           string
	On, Off, Yes, No                                           string
	TSUp, TSDown                                               string
	OpenMainWindow, Quit                                       string
	WritesDisabledHint                                         string
	ErrorPrefix                                                string

	// Backend states.
	TSStates map[string]string

	// Install kinds.
	InstallKinds map[string]string

	// Main window pages.
	PageOverview, PageProxies, PageTailscale, PageHealth, PageLogs, PageSettings string

	// Proxies page.
	TestGroupDelay, TestingDelay, Members, Selected, NoGroups string
	TestAllDelay                                              string
	TestAllProgress                                           string // nodes done, nodes in all

	// Tailscale page.
	PeerName, PeerIP, PeerOS, PeerPath, PeerLastSeen, PeerRelay string
	PathDirect, PathDERP, PathPeerRelay, PathIdle               string
	Online, Offline, NoPeers, CopyIP                            string

	// Health page.
	HealthSummary                       string // %d ok, warn, fail, skip
	HealthLastRun                       string // %s: time, %s: duration
	HealthRunning, HealthRerun          string
	HealthCopyReport, Copied            string
	LearnMore                           string // the link in an ⓘ popover (infoTip)
	HealthEvidence, HealthFix, CopyCmd  string
	HealthNever, HealthUnavailable      string
	HealthError                         string // %s
	HealthOK, HealthWarn, HealthFail    string
	HealthSkip                          string
	PanelHealthOK, PanelHealthWarn      string // %d
	PanelHealthFail, PanelHealthPending string // %d
	PanelPeers                          string // %d online, %d in all
	// ⓘ of the flyout (infoTip): one to three short sentences each.
	InfoTun, InfoExitNode, InfoRecovery string
	Seconds                             string // %.1f

	// Controls (v0.2).
	Actions, ReloadConfig, FlushFakeIP, FlushDNS, Done string
	ExitNodeNone, ExitNodeConflictTitle                string
	ExitNodeConflictBody, ExitNodeConfirm, Cancel      string
	TestNodeDelay                                      string

	// Logs page.
	LogLevel, LogClear, LogPause, LogResume, LogEmpty string

	// Settings page.
	Controller, Secret, ConfigFile, Save, Saved, SaveFailed              string
	ControllerFromHelper                                                 string
	MihomoBinary, MihomoConfigDir, TailscaleSocket, AllowWrites          string
	Notifications, NotificationsUnsupported, NotificationsUnsupportedTip string

	// Helper connection.
	Helper, HelperChecking, HelperConnected, HelperNotInstalled string
	HelperNotRunning, HelperDenied, HelperNotConnectedErr       string
	HelperDeniedHint                                            string
	HelperRetry, HelperSocket, Subscription, LastApply          string
	OpDone                                                      string // %s: operation
	Loading, Close, Remove, Or, Unsaved, Checking, BinaryPath   string
	ReloadReason                                                string

	// Pages backed by the helper.
	PageConfig, PageHistory, PageKernel string

	// Subscriptions.
	Subscriptions, SubAdd, SubNone, SubActive, SubRefresh, SubActivate string
	SubRemoveTitle                                                     string // %s: name
	SubRemoveBody, SubFromURL, SubFromFile, SubManual                  string
	SubEvery                                                           string // %d: minutes
	SubNeverFetched, SubFetched, SubChanged                            string // %s: ago
	SubID, SubName, SubNamePlaceholder, SubInterval, SubURL            string
	SubImportFile, SubURLHint, SubSave                                 string
	SubFileChosen                                                      string // %s: file, %s: size

	// Layers and the layer editor.
	Layers, LayerNew, LayerEdit, LayerGenerated, LayerEnabled             string
	LayerManaged, LayerManagedHint, LayerID, LayerName, LayerStage        string
	LayerAdvanced                                                         string
	LayerFormat, FormatMerge, FormatVerge, MoveUp, MoveDown, LayerReorder string
	LayerToggle                                                           string // %s: layer
	LayerRemoveTitle                                                      string // %s: layer
	LayerTemplate                                                         string
	Stages                                                                map[string]string

	// Override lint (status.LintOverride).
	LintYAMLError                        string // %s
	LintPrepend, LintAppend, LintReplace string // %s key, %s target
	LintNeedsList, LintLooksLiteral      string // %s key, %s key
	LintLine                             string // %d

	// Preview and apply.
	PreviewApply, Preview, DiffStats, DiffUnavailable               string
	ConfigTextUnsupported, ComposeLayers, ComposeHash, ComposeValid string
	ComposeChanged, ValidOK, ValidFail, ApplyReason, Apply          string
	ApplyReasonPlaceholder, ApplyReasonDefault                      string
	ApplyDone, ApplyFailed, ApplyUnchanged, ApplyWritten            string
	ApplyReloaded, ApplyRolledBack                                  string

	// History page.
	HistoryEmpty, HistoryTime, HistoryKind, HistorySize, HistoryHash string
	HistoryReason, HistoryPick, HistoryEntry, HistoryDiff            string
	HistoryDiffHint, HistoryRollback, HistoryRollbackBody            string
	HistoryRollbackTitle                                             string // %s: time
	HistoryKinds                                                     map[string]string

	// Kernel page.
	KernelMihomo, KernelActive, KernelExternal, KernelRunning, KernelCurrent                    string
	KernelPrevious, KernelInstalled, KernelChannel, ChannelStable                               string
	ChannelAlpha, KernelCheck, KernelRollback, KernelRollbackBody                               string
	KernelRollbackTitle, KernelUpdate, KernelInstall, KernelInstallTitle                        string // %s: version
	KernelFirstInstall, KernelUpToDate, KernelLatest, KernelAsset, KernelDigest, KernelNoDigest string
	KernelInstallOK                                                                             string
	TSNotInstalled, UpdateAvailable                                                             string
	TSInstallKinds, TSUpdateHints                                                               map[string]string

	// Helper warnings (warnings.go): kind → template, captures in order.
	WarningTexts map[string]string
	ListSep      string // joins a list in running text

	// Services card (kernel page).
	Services, Refresh, ServiceRunning, ServiceStopped, ServiceLoaded            string
	ServiceNotInstalled, ServiceStart, ServiceStop, ServiceRestart              string
	ServiceInstall, ServiceUninstall, ServiceLabel, ServiceExit, ServiceProgram string
	ServiceDefinition, ServiceDryRun, ServiceDryRunHint                         string
	ServiceStopTitle, ServiceRestartTitle, ServiceInstallTitle                  string // %s: service
	ServiceUninstallTitle                                                       string // %s: service
	ServiceStopBody, ServiceRestartBody                                         string
	ServiceUninstallBody                                                        string // %s: label, %s: plist
	ServiceOpHead                                                               string // %s: op, %s: service

	// Geo databases and external UI cards.
	GeoTitle, UITitle, AccessoryDir, AccessoryForce                                                  string
	AccessoryUpdate, AccessoryUpToDate, AccessoryCurrent, AccessoryError                             string
	AccessoryMissing, AccessoryVerifiable, AccessoryUnverifiable, AccessoryLocal, AccessoryUnchecked string
	AccessoryRemote, AccessoryNothing, AccessoryRolledBack                                           string
	AccessoryUpdated, AccessorySkipped                                                               string // %s: names
	AccessoryReloadNeeded, AccessoryApply, AccessoryApplyReason                                      string
	HelperTooOld                                                                                     string

	// Takeover page.
	PageTakeover, TakeoverFixture, TakeoverTooOld                                     string
	TakeoverPlan, TakeoverPlans, TakeoverOpen                                         string
	TakeoverDiscardTitle                                                              string // %s: plan id
	TakeoverDiscardBody, TakeoverFound, TakeoverLive, TakeoverMihomoSvc               string
	TakeoverBinary, TakeoverSecretSet                                                 string
	TakeoverTSSvc, TakeoverBase                                                       string
	TakeoverUseBase                                                                   string
	TakeoverCandScore                                                                 string // proxies same/total, groups same/total, tail/rules, head
	TakeoverNoLayers                                                                  string
	TakeoverRules                                                                     string // %d
	TakeoverVerdict, TakeoverEquivalent, TakeoverNotEquivalent, TakeoverDeliberate    string
	TakeoverOtherChanges, TakeoverNoChanges, TakeoverShowText, TakeoverHideText       string
	TakeoverAdopt                                                                     string
	TakeoverAdoptedAt                                                                 string // %s: time
	TakeoverReplan, TakeoverNotValid                                                  string
	TakeoverCheckChanges, TakeoverAdoptTitle                                          string
	TakeoverAdoptBody                                                                 string // %s: live config path
	TakeoverResult, TakeoverDone, TakeoverRolledBack, TakeoverFailed                  string
	TakeoverHealth, TakeoverHealthBefore, TakeoverHealthAfter, TakeoverHealthRollback string
	PlanStates                                                                        map[string]string
	SecureTitle, SecureRun, SecureBody, SecureDone, SecureReplaced                    string

	// First run (helper not installed).
	OnboardTitle, OnboardIntro, OnboardReadOnly, OnboardPlan, OnboardPlanHint string
	OnboardInstall, OnboardInstalling, OnboardOpen, CLINotFound               string
	OnboardPoints                                                             []string
	OnboardSteps, OnboardShowFile, OnboardHideFile                            string
	PlanMkdir, PlanCopy, PlanWrite, PlanRemove                                string // %s: path
	PlanBootout, PlanBootstrap, PlanEnable, PlanDaemonReload                  string
	PlanWaitStopped, PlanKickstart                                            string

	// Units and time.
	JustNow, SecondsAgo, MinutesAgo, HoursAgo, DaysAgo string // %d
	InDays, InHours                                    string // %d

	// Sidebar sections, empty states, reload impact.
	SidebarHelper, SidebarApp          string
	LogEmptyHint, NoGroupsHint         string
	ImpactGentle, ImpactDisruptive     string
	ImpactRestart, ImpactLabel         string
	ReadOnlyShort, TSConnect, Devices  string
	SecretFromEnv, NotificationsSwitch string
	MenuBar, TraySpeedSwitch           string
	TraySpeedLayout                    string
	TraySpeedOneLine, TraySpeedTwo     string
	AboutApp                           string
	HealthProblems, HealthPassed       string

	// Texts kept in their own files (one per area, so parallel work does
	// not collide here). Fields are promoted: T.X reads them.
	ControlStrings         // controls.go
	ConfigStrings          // config.go
	WindowStrings          // window.go
	AboutStrings           // about.go
	TakeoverStrings        // takeover.go
	HelperMgmtStrings      // helpermgmt.go
	ConfigUIStrings        // configui.go
	LifecycleStrings       // lifecycle.go
	TakeoverExplainStrings // takeoverexplain.go
	InfoTipStrings         // infotips.go
	StatusStrings          // status.go
	SidebarStrings         // sidebar.go
	ErrorStrings           // errors.go
}

// zh is the Chinese text.
var zh = Strings{
	ControlStrings: zhControls,
	ErrorStrings:   zhErrors,

	AppName: "duet",

	StatusOK:      "一切正常",
	StatusWarn:    "需要注意",
	StatusError:   "出现故障",
	StatusUnknown: "正在检测",

	ReasonMihomoDown:     "连不上 mihomo 控制接口",
	ReasonMihomoAuth:     "mihomo 控制接口的密钥不对",
	ReasonTunOff:         "mihomo TUN 未开启",
	ReasonPrimaryDead:    "策略组「%s」当前节点测速失败",
	ReasonTSDown:         "连不上 Tailscale",
	ReasonTSNotInstalled: "没有找到 Tailscale",
	ReasonTSNeedsLogin:   "Tailscale 需要登录",
	ReasonTSStopped:      "Tailscale 已断开",
	ReasonTSExitNode:     "Tailscale 正在使用出口节点「%s」，可能和代理冲突",
	ReasonTSKeyExpiry:    "Tailscale 密钥将在 %s 过期",
	ReasonTSHealth:       "Tailscale：%s",
	ReasonTSSystemVPN:    "Tailscale 使用系统 VPN 通道，建议改用 tailscaled",
	ReasonHealthWarn:     "健康检查：%d 项警告",
	ReasonHealthFail:     "健康检查：%d 项失败",

	AlertMihomoDown:      "mihomo 无法连接",
	AlertTSNeedsLogin:    "Tailscale 需要登录",
	AlertTSKeyExpiry:     "Tailscale 密钥即将过期",
	AlertTSKeyExpiryBody: "将在 %s 过期（%s），请在管理后台续期或重新登录",
	AlertHealthFail:      "健康检查失败：%s",

	Mihomo:             "mihomo",
	Tailscale:          "Tailscale",
	Running:            "运行中",
	NotRunning:         "未运行",
	Unreachable:        "无法连接",
	Mode:               "模式",
	ModeRule:           "规则",
	ModeGlobal:         "全局",
	ModeDirect:         "直连",
	Tun:                "TUN",
	TunOn:              "已开启",
	TunOff:             "未开启",
	Version:            "版本",
	PrimaryGroup:       "主策略组",
	Node:               "节点",
	Delay:              "延迟",
	Exit:               "出口",
	DelayTimeout:       "超时",
	DelayUnknown:       "未测",
	Traffic:            "流量",
	Upload:             "上传",
	Download:           "下载",
	Total:              "累计",
	TrafficLastSec:     "最近 %d 秒",
	TrafficLastMin:     "最近 %d 分钟",
	Connections:        "连接数",
	State:              "状态",
	SelfIP:             "本机 IP",
	SelfName:           "本机名称",
	OnlinePeers:        "在线设备",
	ExitNode:           "出口节点",
	NoExitNode:         "未使用",
	MagicDNS:           "MagicDNS",
	ShieldsUp:          "Shields Up",
	KeyExpiry:          "密钥过期",
	Account:            "账户",
	Install:            "安装方式",
	On:                 "开",
	Off:                "关",
	Yes:                "是",
	No:                 "否",
	TSUp:               "连接",
	TSDown:             "断开",
	OpenMainWindow:     "打开主窗口",
	Quit:               "退出",
	WritesDisabledHint: "只读模式",
	ErrorPrefix:        "错误：",

	TSStates: map[string]string{
		"NoState":          "未知",
		"NeedsLogin":       "需要登录",
		"NeedsMachineAuth": "等待管理员批准",
		"Stopped":          "已断开",
		"Starting":         "正在连接",
		"Running":          "已连接",
	},
	InstallKinds: map[string]string{
		"tailscaled":     "开源 tailscaled",
		"mac-standalone": "macOS 官方 App（Standalone）",
		"mac-appstore":   "macOS App Store 版",
		"windows":        "Windows 客户端",
		"unknown":        "未知",
	},

	PageOverview:  "总览",
	PageProxies:   "策略组",
	PageTailscale: "Tailscale 设备",
	PageHealth:    "健康检查",
	PageLogs:      "日志",
	PageSettings:  "设置",

	TestGroupDelay:  "测速",
	TestAllDelay:    "全部测速",
	TestingDelay:    "测速中…",
	TestAllProgress: "测速中 %d/%d",
	Members:         "成员",
	Selected:        "当前",
	NoGroups:        "没有策略组",

	PeerName:      "名称",
	PeerIP:        "IP",
	PeerOS:        "系统",
	PeerPath:      "连接方式",
	PeerLastSeen:  "最后在线",
	PeerRelay:     "DERP",
	PathDirect:    "直连",
	PathDERP:      "DERP 中继",
	PathPeerRelay: "节点中继",
	PathIdle:      "未连接",
	Online:        "在线",
	Offline:       "离线",
	NoPeers:       "没有其他设备",
	CopyIP:        "复制 IP",

	HealthSummary:      "通过 %d · 警告 %d · 失败 %d · 跳过 %d",
	HealthLastRun:      "上次检查 %s · 用时 %s",
	HealthRunning:      "正在检查…",
	HealthRerun:        "重新检查",
	HealthCopyReport:   "复制报告",
	Copied:             "已复制",
	LearnMore:          "了解更多",
	HealthEvidence:     "依据",
	HealthFix:          "修复建议",
	CopyCmd:            "复制命令",
	HealthNever:        "还没有检查结果",
	HealthUnavailable:  "这个平台还不支持健康检查",
	HealthError:        "检查出错：%s",
	HealthOK:           "通过",
	HealthWarn:         "警告",
	HealthFail:         "失败",
	HealthSkip:         "跳过",
	PanelHealthOK:      "健康检查：全部通过",
	PanelHealthWarn:    "健康检查：%d 项警告",
	PanelHealthFail:    "健康检查：%d 项失败",
	PanelHealthPending: "健康检查：正在检查",
	PanelPeers:         "%d / %d 台在线",
	InfoTun:            "TUN 让 mihomo 接管本机所有流量，包括不认系统代理的程序。关闭后只有走系统代理的程序经过 mihomo。",
	InfoExitNode:       "让这台 Mac 的流量从 Tailscale 里的另一台设备出去。mihomo 的 TUN 开着时，两者都要接管默认路由，会互相冲突。",
	InfoRecovery:       "网络出问题时的几个快捷操作：重启 mihomo、停用出口节点、重新测试连通性。",
	Seconds:            "%.1f 秒",

	Actions:               "操作",
	ReloadConfig:          "重载配置",
	FlushFakeIP:           "清空 Fake-IP",
	FlushDNS:              "清空 DNS",
	Done:                  "已完成",
	ExitNodeNone:          "不使用",
	ExitNodeConflictTitle: "使用出口节点会和 mihomo 冲突",
	ExitNodeConflictBody:  "出口节点会接管默认路由，和 mihomo 的 TUN 抢同一份流量，代理规则可能失效。仍要使用「%s」吗？",
	ExitNodeConfirm:       "仍然使用",
	Cancel:                "取消",
	TestNodeDelay:         "测速",

	LogLevel:  "级别",
	LogClear:  "清空",
	LogPause:  "暂停",
	LogResume: "继续",
	LogEmpty:  "还没有日志",

	Controller:                  "控制接口地址",
	Secret:                      "密钥",
	ControllerFromHelper:        "由后台助手提供",
	ConfigFile:                  "配置文件",
	Save:                        "保存",
	Saved:                       "已保存",
	SaveFailed:                  "保存失败：",
	MihomoBinary:                "mihomo 程序",
	MihomoConfigDir:             "mihomo 配置目录",
	TailscaleSocket:             "Tailscale 安装",
	AllowWrites:                 "允许操作",
	Notifications:               "系统通知",
	NotificationsUnsupported:    "需打包版",
	NotificationsUnsupportedTip: "macOS 只给打包后的 app 显示通知",

	Helper:                "后台助手",
	HelperChecking:        "正在连接",
	HelperConnected:       "已连接",
	HelperNotInstalled:    "未安装",
	HelperNotRunning:      "未运行",
	HelperDenied:          "无权限",
	HelperNotConnectedErr: "后台助手未连接",
	HelperDeniedHint:      "只有管理员（admin 组）用户可以管理网络配置",
	HelperRetry:           "重新连接",
	HelperSocket:          "套接字",
	Subscription:          "订阅",
	LastApply:             "上次应用",
	OpDone:                "已完成：%s",
	Loading:               "正在加载…",
	Close:                 "关闭",
	Remove:                "删除",
	Or:                    "或",
	Unsaved:               "有未保存的修改",
	Checking:              "正在检查…",
	BinaryPath:            "路径",
	ReloadReason:          "从 app 重载配置",

	PageConfig:  "订阅与覆写",
	PageHistory: "历史",
	PageKernel:  "内核",

	Subscriptions:      "订阅",
	SubAdd:             "添加订阅",
	SubNone:            "还没有订阅",
	SubActive:          "使用中",
	SubRefresh:         "更新",
	SubActivate:        "设为使用中",
	SubRemoveTitle:     "删除订阅「%s」？",
	SubRemoveBody:      "订阅地址和下载的内容会被删除，已应用的配置不受影响。",
	SubFromURL:         "订阅地址已保存",
	SubFromFile:        "本地文件",
	SubManual:          "手动更新",
	SubEvery:           "每 %d 分钟更新",
	SubNeverFetched:    "还没有下载过",
	SubFetched:         "%s下载",
	SubChanged:         "%s有变化",
	SubID:              "ID",
	SubName:            "名称",
	SubNamePlaceholder: "机场名称",
	SubInterval:        "更新间隔（分钟，0 = 手动）",
	SubURL:             "订阅地址",
	SubImportFile:      "从文件导入…",
	SubURLHint:         "保存后不再显示",
	SubSave:            "保存订阅",
	SubFileChosen:      "已选择 %s（%s）",

	Layers:           "覆写",
	LayerNew:         "新建覆写",
	LayerEdit:        "编辑覆写",
	LayerGenerated:   "生成的内容",
	LayerEnabled:     "启用",
	LayerManaged:     "托管",
	LayerManagedHint: "由 duet 生成，只读",
	LayerID:          "ID",
	LayerAdvanced:    "高级",
	LayerName:        "名称",
	LayerStage:       "阶段",
	LayerFormat:      "格式",
	FormatMerge:      "Clash Party（+key / key+）",
	FormatVerge:      "Clash Verge（prepend-rules…）",
	MoveUp:           "上移",
	MoveDown:         "下移",
	LayerReorder:     "调整顺序",
	LayerToggle:      "切换 %s",
	LayerRemoveTitle: "删除覆写「%s」？",
	LayerTemplate:    "# 覆写（Clash Party 语法）\n# +rules 前置，rules+ 追加，key! 整体替换；字面量键写成 '<+.lan>'\n+rules:\n  - DOMAIN-SUFFIX,example.com,DIRECT\n",
	Stages: map[string]string{
		"base":    "基础",
		"user":    "用户",
		"coexist": "Tailscale 共存",
		"guard":   "防泄漏",
	},

	LintYAMLError:    "YAML 错误：%s",
	LintPrepend:      "%s：前置到 %s 列表",
	LintAppend:       "%s：追加到 %s 列表",
	LintReplace:      "%s：整体替换 %s",
	LintNeedsList:    "%s 会被当作合并指令，但值不是列表，应用时会失败；如果是字面量键，请写成 '<%s>'",
	LintLooksLiteral: "%s 像是域名或地址，却会被当作合并指令；如果是字面量键，请写成 '<%s>'",
	LintLine:         "第 %d 行：",

	PreviewApply:           "预览与应用",
	Preview:                "预览",
	DiffStats:              "新增 %d 行 · 删除 %d 行",
	DiffUnavailable:        "无法显示差异：%s",
	ConfigTextUnsupported:  "后台助手版本较旧，不能显示配置全文",
	ComposeLayers:          "合成顺序",
	ComposeHash:            "哈希",
	ComposeValid:           "mihomo -t",
	ComposeChanged:         "有变化",
	ValidOK:                "通过",
	ValidFail:              "未通过",
	ApplyReason:            "说明",
	Apply:                  "应用",
	ApplyReasonPlaceholder: "例如：加了公司内网规则",
	ApplyReasonDefault:     "从 app 应用",
	ApplyDone:              "已应用",
	ApplyFailed:            "应用失败",
	ApplyUnchanged:         "没有变化，未写入",
	ApplyWritten:           "写入",
	ApplyReloaded:          "重载",
	ApplyRolledBack:        "新配置出错，已恢复旧配置",

	HistoryEmpty:         "还没有历史记录",
	HistoryTime:          "时间",
	HistoryKind:          "类型",
	HistorySize:          "大小",
	HistoryHash:          "哈希",
	HistoryReason:        "说明",
	HistoryPick:          "选一条记录查看",
	HistoryEntry:         "记录",
	HistoryDiff:          "与当前对比",
	HistoryDiffHint:      "这条记录 → 当前配置",
	HistoryRollback:      "回滚到这里",
	HistoryRollbackTitle: "回滚到 %s 的配置？",
	HistoryRollbackBody:  "mihomo 会重载；当前配置保留在历史里。",
	HistoryKinds: map[string]string{
		"applied":  "应用",
		"backup":   "备份",
		"rollback": "回滚",
	},

	KernelMihomo:        "mihomo 内核",
	KernelActive:        "使用中",
	KernelExternal:      "外部程序",
	KernelRunning:       "运行版本",
	KernelCurrent:       "当前",
	KernelPrevious:      "上一个",
	KernelInstalled:     "已安装",
	KernelChannel:       "通道",
	ChannelStable:       "稳定版",
	ChannelAlpha:        "Alpha",
	KernelCheck:         "检查更新",
	KernelRollback:      "回滚内核",
	KernelRollbackTitle: "回滚到内核 %s？",
	KernelRollbackBody:  "下次启动 mihomo 时生效。",
	KernelUpdate:        "有新版本 %s",
	KernelUpToDate:      "已是最新",
	KernelFirstInstall:  "可以安装由 duet 管理的内核 %s",
	KernelLatest:        "最新版本",
	KernelAsset:         "文件",
	KernelDigest:        "SHA-256",
	KernelNoDigest:      "无校验值，不能安装",
	KernelInstall:       "安装 %s",
	KernelInstallTitle:  "安装 mihomo %s？",
	KernelInstallOK:     "安装",
	TSNotInstalled:      "未安装",
	UpdateAvailable:     "可更新",
	TSInstallKinds: map[string]string{
		"brew":       "Homebrew",
		"standalone": "官方 App（Standalone）",
		"appstore":   "App Store",
		"static":     "静态二进制",
		"unknown":    "未知",
	},
	TSUpdateHints: map[string]string{
		"brew":       "用 brew upgrade tailscale 更新，然后重启 tailscaled。",
		"standalone": "在 Tailscale App 的菜单里检查更新。",
		"appstore":   "在 App Store 里更新。",
		"static":     "从 pkgs.tailscale.com 下载新版替换。",
	},

	WarningTexts: map[string]string{
		"group-missing":    "防泄漏：找不到策略组「%s」",
		"group-empty":      "防泄漏：策略组「%s」去掉直连后没有成员了，已改为 REJECT",
		"group-direct":     "防泄漏：策略组「%s」仍能经由 %s 走直连（嵌套策略组还承载别的流量，没有改动）",
		"nested-shared":    "防泄漏：嵌套策略组「%s」没有成员时会走直连（它还承载别的流量，没有改动）",
		"nested-direct":    "防泄漏：嵌套策略组「%s」没有成员时会走直连；开启 leak_guard_nested 可以修复",
		"fakeip-whitelist": "dns.fake-ip-filter-mode 是 whitelist，没有加入 Tailscale 的 fake-ip-filter 条目；请确认白名单不包含 ts.net 或单段名字",
		"fakeip-unknown":   "不认识的 dns.fake-ip-filter-mode「%s」，没有加入 Tailscale 的 fake-ip-filter 条目",
		"new-secret":       "已生成新的控制接口密钥",
		"reload-disrupts":  "这次重载会短暂中断网络",
		"restart-needed":   "%s 要在 mihomo 重启后才生效",
		"mihomo-stopped":   "mihomo 没有运行（%s）；配置已写入，下次启动时生效",
		"exposed":          "%s：%s 可以从网络访问，只有密钥保护",
		"adopt-refuses":    "接管会拒绝这套配置：%s",
		"adopt-profiles":   "找到 %s 个订阅配置；订阅地址不会导入，接管后在「订阅与覆写」添加",
		"adopt-disrupts":   "接管时的重载会短暂中断网络",
		"adopt-differs":    "做计划之后 tailnet 设备或网卡有变化，接管时已按现在的情况生成配置，不用重新生成计划",
		"binary-writable":  "mihomo 程序 %s 不需要 root 就能被替换（%s），而服务以 root 运行它；请在「内核」安装由 duet 管理的内核",
		"plain-http":       "订阅地址是明文 http，可能被读取或篡改；服务商支持的话请改用 https",
		"secret-clients":   "网页面板、脚本等客户端需要新密钥，可用 %s 读取",
	},
	ListSep: "、",

	Services:              "服务",
	Refresh:               "刷新",
	ServiceRunning:        "运行中",
	ServiceStopped:        "已停止",
	ServiceLoaded:         "已加载，未运行",
	ServiceNotInstalled:   "未安装",
	ServiceStart:          "启动",
	ServiceStop:           "停止",
	ServiceRestart:        "重启",
	ServiceInstall:        "安装服务",
	ServiceUninstall:      "卸载服务",
	ServiceLabel:          "标签",
	ServiceExit:           "上次退出码",
	ServiceProgram:        "程序",
	ServiceDefinition:     "定义文件",
	ServiceDryRun:         "仅预演",
	ServiceDryRunHint:     "后台助手不是以 root 运行，步骤没有执行",
	ServiceStopTitle:      "停止「%s」？",
	ServiceRestartTitle:   "重启「%s」？",
	ServiceInstallTitle:   "为「%s」安装 LaunchDaemon？",
	ServiceUninstallTitle: "卸载「%s」服务？",
	ServiceStopBody:       "依赖它的网络会中断，直到再次启动。",
	ServiceRestartBody:    "重启时网络会短暂中断。",
	ServiceUninstallBody:  "网络会中断。内核和配置文件保留。",
	ServiceOpHead:         "%s %s",

	GeoTitle:              "GEO 数据库",
	UITitle:               "控制面板（external UI）",
	AccessoryDir:          "目录",
	AccessoryForce:        "强制重新下载",
	AccessoryUpdate:       "更新",
	AccessoryUpToDate:     "已是最新",
	AccessoryCurrent:      "最新",
	AccessoryError:        "出错",
	AccessoryMissing:      "未安装",
	AccessoryVerifiable:   "可校验",
	AccessoryUnverifiable: "无校验值",
	AccessoryUnchecked:    "未检查",
	AccessoryLocal:        "本地",
	AccessoryRemote:       "远端",
	AccessoryNothing:      "没有需要更新的文件",
	AccessoryRolledBack:   "有文件失败，本次替换的文件都已恢复",
	AccessoryUpdated:      "已更新：%s",
	AccessorySkipped:      "跳过：%s",
	AccessoryReloadNeeded: "新数据还没生效",
	AccessoryApply:        "应用以生效",
	AccessoryApplyReason:  "更新 GEO 数据",
	HelperTooOld:          "后台助手版本较旧，不支持这个操作",

	PageTakeover:           "接管",
	TakeoverFixture:        "示例数据，不会发送任何操作",
	TakeoverTooOld:         "后台助手版本较旧，不支持接管",
	TakeoverPlan:           "生成接管计划",
	TakeoverPlans:          "接管计划",
	TakeoverOpen:           "查看",
	TakeoverDiscardTitle:   "删除接管计划「%s」？",
	TakeoverDiscardBody:    "不影响现在的配置。",
	TakeoverFound:          "现有安装",
	TakeoverLive:           "配置文件",
	TakeoverMihomoSvc:      "mihomo 服务",
	TakeoverBinary:         "mihomo 程序",
	TakeoverSecretSet:      "已设密钥",
	TakeoverTSSvc:          "Tailscale 服务",
	TakeoverBase:           "订阅原文",
	TakeoverUseBase:        "改用这个",
	TakeoverCandScore:      "节点 %d/%d · 策略组 %d/%d · 规则尾部 %d/%d（前面另有 %d 条）",
	TakeoverNoLayers:       "没有覆写",
	TakeoverRules:          "%d 条规则",
	TakeoverVerdict:        "对比结果",
	TakeoverEquivalent:     "合成结果与现在的配置行为一致（除下面列出的有意修改）",
	TakeoverNotEquivalent:  "合成结果与现在的配置有差异，请看下面的变化",
	TakeoverDeliberate:     "有意的修改",
	TakeoverOtherChanges:   "其他差异",
	TakeoverNoChanges:      "没有行为差异",
	TakeoverShowText:       "显示完整报告",
	TakeoverHideText:       "收起报告",
	TakeoverAdopt:          "接管",
	TakeoverAdoptedAt:      "已于 %s 接管",
	TakeoverReplan:         "请重新生成计划",
	TakeoverNotValid:       "合成结果没有通过 mihomo -t，不能接管",
	TakeoverCheckChanges:   "有未说明的差异，确认没问题再接管",
	TakeoverAdoptTitle:     "接管现有配置？",
	TakeoverAdoptBody:      "会备份并改写 %s，重载时网络可能短暂中断；健康检查变差会自动恢复。",
	TakeoverResult:         "接管结果",
	TakeoverDone:           "接管完成",
	TakeoverRolledBack:     "健康检查变差，已恢复原配置",
	TakeoverFailed:         "接管失败",
	TakeoverHealth:         "健康检查",
	TakeoverHealthBefore:   "接管前",
	TakeoverHealthAfter:    "接管后",
	TakeoverHealthRollback: "恢复后",
	PlanStates: map[string]string{
		"ready":       "待接管",
		"adopted":     "已接管",
		"rolled_back": "已回滚",
		"failed":      "失败",
	},
	SecureTitle:    "加固控制接口",
	SecureRun:      "生成密钥并应用",
	SecureBody:     "mihomo 下次启动后生效。网页面板、脚本等要手动换新密钥。",
	SecureDone:     "已设置新的控制接口密钥",
	SecureReplaced: "已替换控制接口密钥",

	OnboardTitle: "安装后台助手",
	OnboardIntro: "修改网络配置需要一个以 root 运行的后台助手：",
	OnboardPoints: []string{
		"应用订阅和覆写，出错自动恢复",
		"定时更新订阅，自动和 Tailscale 共存",
		"保留历史，可以对比和回滚",
		"安装和切换 mihomo 内核",
	},
	OnboardReadOnly:   "不安装也能查看状态",
	OnboardPlan:       "安装时会做什么",
	OnboardPlanHint:   "点击安装后会要求输入一次管理员密码",
	OnboardInstall:    "安装后台助手",
	OnboardInstalling: "正在安装…",
	OnboardOpen:       "了解并安装",
	OnboardSteps:      "安装步骤 · %d 步",
	OnboardShowFile:   "显示文件内容",
	OnboardHideFile:   "隐藏文件内容",
	PlanMkdir:         "创建目录 %s",
	PlanCopy:          "把后台助手复制到 %s",
	PlanWrite:         "写入服务定义 %s",
	PlanRemove:        "删除 %s",
	PlanBootout:       "停掉旧的后台助手（如果有）",
	PlanBootstrap:     "加载服务：后台助手以 root 启动",
	PlanEnable:        "设为开机自动启动",
	PlanDaemonReload:  "让 systemd 重新读取服务定义",
	PlanWaitStopped:   "等旧的后台助手完全停下",
	PlanKickstart:     "重新启动后台助手",
	CLINotFound:       "找不到 duet 命令行工具（可用 DUET_CLI 指定）",

	JustNow:    "刚刚",
	SecondsAgo: "%d 秒前",
	MinutesAgo: "%d 分钟前",
	HoursAgo:   "%d 小时前",
	DaysAgo:    "%d 天前",
	InDays:     "%d 天后",
	InHours:    "%d 小时后",

	SidebarHelper:       "配置",
	SidebarApp:          "应用",
	LogEmptyHint:        "调低级别可以看到更多日志",
	NoGroupsHint:        "mihomo 可能还没加载配置",
	ImpactGentle:        "不会中断网络",
	ImpactDisruptive:    "会短暂中断网络",
	ImpactRestart:       "需要重启 mihomo 才生效",
	ImpactLabel:         "网络影响",
	ReadOnlyShort:       "只读",
	TSConnect:           "连接 Tailscale",
	Devices:             "设备",
	SecretFromEnv:       "来自环境变量 DUET_MIHOMO_SECRET",
	NotificationsSwitch: "出现故障时发通知",
	MenuBar:             "菜单栏",
	TraySpeedSwitch:     "在菜单栏显示网速",
	TraySpeedLayout:     "网速显示方式",
	TraySpeedOneLine:    "单行",
	TraySpeedTwo:        "双行",
	AboutApp:            "关于 duet",
	HealthProblems:      "需要处理",
	HealthPassed:        "其他检查",

	ConfigStrings:          zhConfig,
	WindowStrings:          zhWindow,
	AboutStrings:           zhAbout,
	TakeoverStrings:        zhTakeover,
	HelperMgmtStrings:      zhHelperMgmt,
	ConfigUIStrings:        zhConfigUI,
	LifecycleStrings:       zhLifecycle,
	TakeoverExplainStrings: zhTakeoverExplain,
	InfoTipStrings:         zhInfoTips,
	StatusStrings:          zhStatus,
	SidebarStrings:         zhSidebar,
}

// F formats one of the strings.
func F(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// ModeName returns the label of a mihomo mode.
func (s *Strings) ModeName(m string) string {
	switch m {
	case "rule":
		return s.ModeRule
	case "global":
		return s.ModeGlobal
	case "direct":
		return s.ModeDirect
	}
	return m
}

// TSState returns the label of a Tailscale backend state.
func (s *Strings) TSState(st string) string {
	if v, ok := s.TSStates[st]; ok {
		return v
	}
	return st
}

// InstallKind returns the label of a Tailscale install kind.
func (s *Strings) InstallKind(k string) string {
	if v, ok := s.InstallKinds[k]; ok {
		return v
	}
	return k
}

// Path returns the label of a peer path ("direct", "derp", ...).
func (s *Strings) Path(p string) string {
	switch p {
	case "direct":
		return s.PathDirect
	case "derp":
		return s.PathDERP
	case "peer-relay":
		return s.PathPeerRelay
	}
	return s.PathIdle
}

// PlanState returns the label of a takeover plan state.
func (s *Strings) PlanState(st string) string {
	if v, ok := s.PlanStates[st]; ok {
		return v
	}
	return st
}

// StageName returns the label of a layer stage.
func (s *Strings) StageName(st string) string {
	if v, ok := s.Stages[st]; ok {
		return v
	}
	return st
}

// HistoryKindName returns the label of a history entry kind.
func (s *Strings) HistoryKindName(k string) string {
	if v, ok := s.HistoryKinds[k]; ok {
		return v
	}
	return k
}

// TSInstallKind returns the label of the helper's Tailscale install kind.
func (s *Strings) TSInstallKind(k string) string {
	if v, ok := s.TSInstallKinds[k]; ok {
		return v
	}
	return k
}

// TSUpdateHint says how to update Tailscale for an install kind.
func (s *Strings) TSUpdateHint(k string) string { return s.TSUpdateHints[k] }
