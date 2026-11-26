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
