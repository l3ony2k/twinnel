// Package i18n holds every UI string of the app. Chinese is the only
// language for now; add a language by filling another Strings value and
// returning it from For.
package i18n

import "fmt"

// Strings is the full set of UI texts. Fields, not map keys, so that a
// missing text is a compile error.
type Strings struct {
	AppName string

	// Overall status.
	StatusOK, StatusWarn, StatusError, StatusUnknown string

	// Reasons for a status that is not OK.
	ReasonMihomoDown     string // %s: error
	ReasonMihomoAuth     string
	ReasonTunOff         string
	ReasonPrimaryDead    string // %s: group
	ReasonTSDown         string // %s: error
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

	// Tailscale page.
	PeerName, PeerIP, PeerOS, PeerPath, PeerLastSeen, PeerRelay string
	PathDirect, PathDERP, PathPeerRelay, PathIdle               string
	Online, Offline, NoPeers, CopyIP                            string

	// Health page.
	HealthSummary                       string // %d ok, warn, fail, skip
	HealthLastRun                       string // %s: time, %s: duration
	HealthRunning, HealthRerun          string
	HealthCopyReport, Copied            string
	HealthEvidence, HealthFix, CopyCmd  string
	HealthNever, HealthUnavailable      string
	HealthError                         string // %s
	HealthOK, HealthWarn, HealthFail    string
	HealthSkip                          string
	PanelHealthOK, PanelHealthWarn      string // %d
	PanelHealthFail, PanelHealthPending string // %d
	Seconds                             string // %.1f

	// Controls (v0.2, guarded by DUET_ALLOW_WRITES).
	Actions, ReloadConfig, FlushFakeIP, FlushDNS, Done string
	ExitNodeNone, ExitNodeConflictTitle                string
	ExitNodeConflictBody, ExitNodeConfirm, Cancel      string
	TestNodeDelay                                      string

	// Logs page.
	LogLevel, LogClear, LogPause, LogResume, LogEmpty string

	// Settings page.
	Controller, Secret, SecretHint, ConfigFile, Save, Saved, SaveFailed string
	MihomoBinary, MihomoConfigDir, TailscaleSocket, AllowWrites         string
	SettingsRestartHint                                                 string
	Notifications, NotificationsHint, NotificationsUnsupported          string

	// Helper connection.
	Helper, HelperChecking, HelperConnected, HelperNotInstalled string
	HelperNotRunning, HelperDenied, HelperNotConnectedErr       string
	HelperNotRunningHint, HelperDeniedHint                      string
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
	Layers, LayersHint, LayerNew, LayerEdit, LayerGenerated, LayerEnabled string
	LayerManaged, LayerManagedHint, LayerID, LayerName, LayerStage        string
	LayerFormat, FormatMerge, FormatVerge, MoveUp, MoveDown, LayerReorder string
	LayerToggle                                                           string // %s: layer
	LayerRemoveTitle                                                      string // %s: layer
	LayerRemoveBody, LayerTemplate                                        string
	Stages                                                                map[string]string

	// Override lint (status.LintOverride).
	LintYAMLError                        string // %s
	LintPrepend, LintAppend, LintReplace string // %s key, %s target
	LintNeedsList, LintLooksLiteral      string // %s key, %s key
	LintLine                             string // %d

	// Preview and apply.
	PreviewApply, Preview, PreviewHint, DiffStats, DiffUnavailable  string
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
	KernelInstallBody, KernelInstallOK                                                          string
	TSVersionHint, TSNotInstalled, UpdateAvailable                                              string
	TSInstallKinds, TSUpdateHints                                                               map[string]string

	// First run (helper not installed).
	OnboardTitle, OnboardIntro, OnboardReadOnly, OnboardPlan, OnboardPlanHint string
	OnboardInstall, OnboardInstalling, OnboardOpen, CLINotFound               string
	OnboardPoints                                                             []string

	// Units and time.
	JustNow, SecondsAgo, MinutesAgo, HoursAgo, DaysAgo string // %d
	InDays, InHours                                    string // %d
}

// zh is the Chinese text.
var zh = Strings{
	AppName: "duet",

	StatusOK:      "一切正常",
	StatusWarn:    "需要注意",
	StatusError:   "出现故障",
	StatusUnknown: "正在检测",

	ReasonMihomoDown:     "连不上 mihomo 控制接口：%s",
	ReasonMihomoAuth:     "mihomo 控制接口 secret 不对",
	ReasonTunOff:         "mihomo TUN 未开启",
	ReasonPrimaryDead:    "策略组 %s 当前节点测速失败",
	ReasonTSDown:         "连不上 Tailscale：%s",
	ReasonTSNotInstalled: "没有找到 Tailscale",
	ReasonTSNeedsLogin:   "Tailscale 需要登录",
	ReasonTSStopped:      "Tailscale 已断开",
	ReasonTSExitNode:     "Tailscale 正在使用出口节点 %s，可能和代理冲突",
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
	WritesDisabledHint: "只读模式：设置 DUET_ALLOW_WRITES=1 后才能操作",
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

	TestGroupDelay: "测速",
	TestingDelay:   "测速中…",
	Members:        "成员",
	Selected:       "当前",
	NoGroups:       "没有策略组",

	PeerName:      "名称",
	PeerIP:        "IP",
	PeerOS:        "系统",
	PeerPath:      "连接方式",
	PeerLastSeen:  "最后在线",
	PeerRelay:     "DERP",
	PathDirect:    "直连",
	PathDERP:      "DERP 中继",
	PathPeerRelay: "节点中继",
	PathIdle:      "空闲",
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
	Seconds:            "%.1f 秒",

	Actions:               "操作",
	ReloadConfig:          "重载配置",
	FlushFakeIP:           "清空 Fake-IP",
	FlushDNS:              "清空 DNS",
	Done:                  "已完成",
	ExitNodeNone:          "不使用",
	ExitNodeConflictTitle: "使用出口节点会和 mihomo 冲突",
	ExitNodeConflictBody:  "出口节点会接管本机的默认路由，和 mihomo 的 TUN 抢同一份流量：代理规则可能失效，部分连接会直接从出口节点出去。确定要使用 %s 吗？",
	ExitNodeConfirm:       "仍然使用",
	Cancel:                "取消",
	TestNodeDelay:         "测速",

	LogLevel:  "级别",
	LogClear:  "清空",
	LogPause:  "暂停",
	LogResume: "继续",
	LogEmpty:  "暂无日志",

	Controller:               "控制接口地址",
	Secret:                   "Secret",
	SecretHint:               "也可用环境变量 DUET_MIHOMO_SECRET 提供",
	ConfigFile:               "配置文件",
	Save:                     "保存",
	Saved:                    "已保存",
	SaveFailed:               "保存失败：",
	MihomoBinary:             "mihomo 程序",
	MihomoConfigDir:          "mihomo 配置目录",
	TailscaleSocket:          "Tailscale 安装",
	AllowWrites:              "允许操作",
	SettingsRestartHint:      "控制接口设置保存后立即生效",
	Notifications:            "系统通知",
	NotificationsHint:        "mihomo 断开、健康检查新出现失败、Tailscale 需要登录或密钥 3 天内过期时提醒；同一件事 30 分钟内只提醒一次",
	NotificationsUnsupported: "当前以开发方式运行，macOS 只给打包后的 app（mygo build）显示通知",

	Helper:                "后台助手",
	HelperChecking:        "正在连接",
	HelperConnected:       "已连接",
	HelperNotInstalled:    "未安装",
	HelperNotRunning:      "未运行",
	HelperDenied:          "无权限",
	HelperNotConnectedErr: "后台助手未连接",
	HelperNotRunningHint:  "后台助手已安装，但现在连不上。它由 launchd 守护，通常几秒后会自动恢复；一直这样请看日志 /Library/Application Support/duet/logs/helper.log。",
	HelperDeniedHint:      "后台助手拒绝了当前用户：只有管理员组（admin）的用户可以管理网络配置。",
	HelperRetry:           "重新连接",
	HelperSocket:          "Socket",
	Subscription:          "订阅",
	LastApply:             "上次应用",
	OpDone:                "%s：完成",
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
	SubRemoveTitle:     "删除订阅 %s？",
	SubRemoveBody:      "订阅地址和已下载的内容都会从后台助手删除，已应用的配置不受影响。",
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
	SubURLHint:         "订阅地址只交给后台助手保存，保存后不会再显示。保存后会立即下载一次。",
	SubSave:            "保存订阅",
	SubFileChosen:      "已选择 %s（%s）",

	Layers:           "覆写",
	LayersHint:       "按顺序叠加在订阅之上；托管层由 duet 生成",
	LayerNew:         "新建覆写",
	LayerEdit:        "编辑覆写",
	LayerGenerated:   "生成的内容",
	LayerEnabled:     "启用",
	LayerManaged:     "托管",
	LayerManagedHint: "托管层由 duet 根据 Tailscale 状态和设置生成，只读。Tailscale 共存可以用左侧开关关闭。",
	LayerID:          "ID",
	LayerName:        "名称",
	LayerStage:       "阶段",
	LayerFormat:      "格式",
	FormatMerge:      "Clash Party",
	FormatVerge:      "Verge Merge",
	MoveUp:           "上移",
	MoveDown:         "下移",
	LayerReorder:     "调整顺序",
	LayerToggle:      "切换 %s",
	LayerRemoveTitle: "删除覆写 %s？",
	LayerRemoveBody:  "删除后需要重新应用才会从当前配置里去掉。",
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
	PreviewHint:            "预览会合成订阅和覆写，用 mihomo -t 校验，并和当前生效的配置对比；不会写入任何文件。",
	DiffStats:              "新增 %d 行 · 删除 %d 行",
	DiffUnavailable:        "无法显示差异：%s",
	ConfigTextUnsupported:  "后台助手版本较旧，还不能读取配置全文（config.get）",
	ComposeLayers:          "合成顺序",
	ComposeHash:            "哈希",
	ComposeValid:           "mihomo -t",
	ComposeChanged:         "有变化",
	ValidOK:                "通过",
	ValidFail:              "未通过",
	ApplyReason:            "说明（写入历史）",
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
	HistoryPick:          "点一行查看、对比或回滚",
	HistoryEntry:         "历史",
	HistoryDiff:          "与当前对比",
	HistoryDiffHint:      "旧：这条记录 · 新：当前生效的配置",
	HistoryRollback:      "回滚到这里",
	HistoryRollbackTitle: "回滚到 %s 的配置？",
	HistoryRollbackBody:  "后台助手会校验并写入这份配置、重载 mihomo；失败时自动恢复。当前配置会留在历史里。",
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
	KernelRollbackBody:  "切换内核不会重启 mihomo 服务，下次启动时生效。",
	KernelUpdate:        "有新版本 %s",
	KernelUpToDate:      "已是最新",
	KernelFirstInstall:  "可以安装由 duet 管理的内核 %s（现在用的是外部程序）",
	KernelLatest:        "最新版本",
	KernelAsset:         "文件",
	KernelDigest:        "SHA-256",
	KernelNoDigest:      "（GitHub 没有提供校验值，不会安装）",
	KernelInstall:       "安装 %s",
	KernelInstallTitle:  "安装 mihomo %s？",
	KernelInstallBody:   "后台助手从 GitHub 下载、校验 SHA-256 并检查版本后切换；旧版本保留，可以回滚。不会重启正在运行的 mihomo。",
	KernelInstallOK:     "安装",
	TSVersionHint:       "Tailscale 由它自己的方式更新（Homebrew、App Store 或官方安装包），这里只显示版本。",
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

	OnboardTitle: "安装后台助手",
	OnboardIntro: "改网络配置需要管理员权限。duet 把这些操作交给一个常驻的后台助手（root 运行的 LaunchDaemon），app 本身不需要权限：",
	OnboardPoints: []string{
		"合成订阅和覆写、用 mihomo -t 校验后写入配置并重载，失败自动恢复",
		"定时更新订阅、跟随 Tailscale 状态生成共存规则",
		"保存每次应用的历史，可以对比和回滚",
		"下载、校验和切换 mihomo 内核",
	},
	OnboardReadOnly:   "不安装也可以用：总览、策略组、Tailscale、健康检查和日志都只读取状态。",
	OnboardPlan:       "安装时会做什么",
	OnboardPlanHint:   "下面是 duet helper install --dry-run 的输出，什么都没有执行。点击安装后会弹出一次管理员密码框。",
	OnboardInstall:    "安装后台助手",
	OnboardInstalling: "正在安装…",
	OnboardOpen:       "了解并安装",
	CLINotFound:       "找不到 duet 命令行工具（可用 DUET_CLI 指定）",

	JustNow:    "刚刚",
	SecondsAgo: "%d 秒前",
	MinutesAgo: "%d 分钟前",
	HoursAgo:   "%d 小时前",
	DaysAgo:    "%d 天前",
	InDays:     "%d 天后",
	InHours:    "%d 小时后",
}

// For returns the strings of a language tag; unknown tags get Chinese.
func For(lang string) *Strings { return &zh }

// T is the current language's strings.
var T = For("zh")

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
