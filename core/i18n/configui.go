package i18n

import "strings"

// Texts of batch C (design review 2026-11-27): the 订阅与覆写 page's
// compose groups, drift and pending rows, the 历史 page's reasons, the
// 日志 page's duet events, and the helper messages coded by core/msg.
// Both languages live here, so this area's work does not touch en.go.
//
// Glossary (U59; docs/i18n-glossary.md has the English side):
//   - 订阅: the remote base. 订阅原文 is the file takeover used as the
//     base. Never 基础订阅.
//   - 覆写: any layer. Never 层 or 分层 in the UI.
//   - 基础设置: the base-stage managed layer.
//   - 托管: "duet 生成，只能开关". Only as the layer chip.
//   - 接管: the takeover act; chip 「接管」 for adopted services/kernels.
//   - 控制接口: the external-controller (its address is a row 「地址」).

// ConfigUIStrings holds batch C's texts.
type ConfigUIStrings struct {
	// Compose groups of the 覆写 list, in compose order (U33).
	GroupSubscription, GroupBase, GroupUser, GroupManaged string

	// The 订阅与覆写 page's structure (config-polish, 2026-11-27): the
	// apply card's states, the empty page, the managed layers' own views,
	// and the ⓘ texts that replace subtitles (AGENTS.md).
	ManagedTitle               string // the managed layers' section
	OverridesInfo, ManagedInfo string // ⓘ of the two sections
	CoexistInfo, LeakGuardInfo string // ⓘ of the managed rows
	DriftInfo                  string // ⓘ of the drift state
	ConfigEmptyTitle           string // the page with no subscription
	ConfigEmptyHint            string
	ConfigEmptyTakeover        string // button: to 接管
	StateInSync                string // the apply card: nothing to apply
	StateUnknown               string // the check failed
	NothingToApply             string // tooltip of the disabled 应用
	PreviewHide                string
	CoexistRanges, CoexistDNS  string // kv labels of the coexist view
	CoexistDevices             string
	CoexistDevicesN            string // %d: devices (row meta)
	CoexistNoTailnet           string
	GuardGroups, GuardPick     string
	GuardNone, GuardNoGroups   string
	GuardMeta                  string // %s: the groups
	GuardNoCandidates          string
	ShowGenerated              string

	// An override's row meta: what it does (config-polish2). The singular
	// forms are for 1 (English); %d: count, %s: keys.
	SumRule, SumRules       string
	SumProxy, SumProxies    string
	SumGroup, SumGroups     string
	SumChanges, SumMoreKeys string
	SumEmpty                string
	// A subscription's update interval in human units.
	SubDaily, SubEveryDays   string // %d: days
	SubHourly, SubEveryHours string // %d: hours
	SubEveryMins             string // %d: minutes

	// Subscription row (U32).
	SubSourceAdopted     string // %s: the base file, or "" (then SubSourceAdoptedBare)
	SubSourceAdoptedBare string
	SubUpdated           string // %s: when, %s: size
	SubCopyHash          string

	// Layer editor (U35, U36, U39).
	StageUserLong, StageBaseLong string
	LayerSaveBlocked             string // tooltip of a disabled 保存: the editor has an error
	LayerRevert                  string // ⋯ menu: back to the applied version
	LayerRevertTitle             string // %s: layer
	LayerMore                    string // the ⋯ button's spoken label

	// Pending changes (U39).
	PendingRow        string // %d: count, %s: names
	PendingSub        string // the active subscription changed (or its content)
	PendingSettings   string // leak guard groups changed
	PendingRemoved    string // %s: a layer that was applied and is deleted now
	Discard           string
	DiscardTitle      string
	DiscardRemoves    string // %s: names of layers that will be deleted
	DiscardRestores   string // %s: names of layers set back
	PendingUnknownRow string // pending, but no override differs (the tailnet, a subscription refresh)

	// Apply (U38, H6).
	ReasonEnable, ReasonDisable, ReasonEdit string // %s: layer
	ReasonEditN                             string // %d: count
	ReasonOptional                          string // placeholder of the 说明 field
	AppliedAt                               string // %s: time
	ViewInHistory                           string
	ComposeOrder                            string // label: 合成顺序

	// Drift after a rollback (U41).
	DriftRow         string // %s: time of the entry
	DriftRowBackup   string // %s: time of the entry (a backup: no overrides to restore)
	DriftRestore     string
	DriftReapply     string
	DriftRestoreBody string
	RollbackDisables string // %s: names
	RollbackEnables  string // %s: names
	RollbackChanges  string // %s: names (content differs)
	RollbackNoLayers string // the entry is a plain file: the overrides will not describe it

	// History (U40).
	HistReasons     map[string]string // core/store Reason… code → template (%s: args)
	HistBeforeDuet  string            // chip
	HistCopyHash    string
	HistOverrides   string // detail label: the overrides the entry was composed from
	HistNoManifest  string
	HistRollbackRef string // the time a rollback went back to, when the entry is gone

	// Helper messages coded by core/msg (U57): code → template; the
	// arguments in the order core/msg captures them. WarningTexts (i18n.go)
	// holds the warning codes; this the errors.
	MsgTexts       map[string]string
	MsgUnknown     string // an error the app has no words for
	MsgUnknownWarn string // a warning the app has no words for
	MsgCopyRaw     string

	// Reload impact (rpc.ReloadImpact fields).
	ImpactTUN       string // %s: what changed
	ImpactTUNWhy    map[string]string
	ImpactDNS       string
	ImpactListeners string // %s: keys
	ImpactProviders string // %s: names
	ImpactSelection string
	ImpactUnknown   string
	// An apply that mihomo -t rejected: where the name it quotes comes
	// from. %s: the override, the name / the name.
	CulpritLayer, CulpritSub, CulpritOpen string
	ApplyFailedRestore                    string // the new config failed after writing; the old one runs

	// Logs: the duet tab (U29, H7).
	LogsSourceMihomo, LogsSourceDuet string
	LogSearch                        string
	LogNoMatch                       string
	EventsEmpty                      string
	EventKinds                       map[string]string // event type → chip
	EvApplied, EvApplyUnchanged      string            // %s: reason
	EvApplyFailed                    string            // %s: reason
	EvState                          string
	EvSubUpdated, EvSubSame          string // %s: subscription
	EvSubFailed                      string // %s: subscription
	EvTailnet, EvKernel, EvAccessory string
	EvCheck                          string // %s: check, %s: status
	EvHealth                         string // %d: failed, %d: warnings
	EvService, EvTakeover            string
	EvHelperStarted, EvHelperStop    string
	EvShow                           string // toast action: open the event in 日志 → duet
}

var zhConfigUI = ConfigUIStrings{
	GroupSubscription: "订阅",
	GroupBase:         "基础",
	GroupUser:         "用户覆写",
	GroupManaged:      "托管",

	ManagedTitle:        "duet 托管",
	OverridesInfo:       "覆写按从上到下的顺序改写订阅，后面的可以覆盖前面的。拖动一行可以调整顺序。",
	ManagedInfo:         "这些覆写由 duet 按 Tailscale 和设置生成并保持最新，只能开关。它们总排在你的覆写之后。",
	CoexistInfo:         "让 mihomo 的 TUN 和 Tailscale 同时工作：tailnet 网段和设备名直连，MagicDNS 名称交给 Tailscale 解析。",
	LeakGuardInfo:       "从所选策略组中去掉 DIRECT，这些组的流量就不会绕过代理。组里没有别的节点时改为 REJECT。",
	DriftInfo:           "回滚后，mihomo 运行的是历史版本，不是这些覆写合成的结果。重新应用之前，订阅和 Tailscale 变化不会自动应用。",
	ConfigEmptyTitle:    "还没有订阅",
	ConfigEmptyHint:     "添加订阅链接或文件，或者接管这台 Mac 上已有的 mihomo 配置",
	ConfigEmptyTakeover: "接管现有配置",
	StateInSync:         "与运行中的配置一致",
	StateUnknown:        "没能比较运行中的配置",
	NothingToApply:      "没有未应用的改动",
	PreviewHide:         "收起预览",
	CoexistRanges:       "直连网段",
	CoexistDNS:          "MagicDNS",
	CoexistDevices:      "设备",
	CoexistDevicesN:     "%d 台设备",
	CoexistNoTailnet:    "还没有读到 Tailscale 的设备",
	GuardGroups:         "受保护的策略组",
	GuardPick:           "选择策略组",
	GuardNone:           "还没有受保护的策略组",
	GuardNoGroups:       "未选择策略组",
	GuardMeta:           "保护 %s",
	GuardNoCandidates:   "没有含 DIRECT 的策略组",
	ShowGenerated:       "查看生成内容",

	SumRule:       "%d 条规则",
	SumRules:      "%d 条规则",
	SumProxy:      "%d 个节点",
	SumProxies:    "%d 个节点",
	SumGroup:      "%d 个策略组",
	SumGroups:     "%d 个策略组",
	SumChanges:    "改 %s",
	SumMoreKeys:   " 等 %d 项",
	SumEmpty:      "空",
	SubDaily:      "每天更新",
	SubEveryDays:  "每 %d 天更新",
	SubHourly:     "每小时更新",
	SubEveryHours: "每 %d 小时更新",
	SubEveryMins:  "每 %d 分钟更新",

	SubSourceAdopted:     "来源：接管时的配置（%s）",
	SubSourceAdoptedBare: "来源：接管时的配置",
	SubUpdated:           "上次更新 %s · %s",
	SubCopyHash:          "复制哈希",

	StageUserLong:    "订阅之后（用户）",
	StageBaseLong:    "订阅之前的基础设置",
	LayerSaveBlocked: "先改正标红的行",
	LayerRevert:      "还原为已应用版本",
	LayerRevertTitle: "把「%s」还原为已应用的版本？",
	LayerMore:        "更多",

	PendingRow:        "%d 个覆写有未应用的改动：%s",
	PendingSub:        "订阅",
	PendingSettings:   "防泄漏",
	PendingRemoved:    "%s（已删除）",
	Discard:           "放弃改动",
	DiscardTitle:      "放弃未应用的改动？",
	DiscardRemoves:    "将删除：%s",
	DiscardRestores:   "将还原：%s",
	PendingUnknownRow: "有未应用的改动",

	ReasonEnable:   "启用 %s",
	ReasonDisable:  "停用 %s",
	ReasonEdit:     "改动 %s",
	ReasonEditN:    "改动 %d 个覆写",
	ReasonOptional: "可不填",
	AppliedAt:      "已应用 · %s",
	ViewInHistory:  "在历史中查看",
	ComposeOrder:   "合成顺序",

	DriftRow:         "运行中：%s 的历史版本（不是由下面的覆写合成）",
	DriftRowBackup:   "运行中：%s 的备份（不是由下面的覆写合成）",
	DriftRestore:     "恢复这些覆写的状态",
	DriftReapply:     "重新应用当前覆写",
	DriftRestoreBody: "只改覆写，不会应用。",
	RollbackDisables: "将停用：%s",
	RollbackEnables:  "将启用：%s",
	RollbackChanges:  "内容不同：%s",
	RollbackNoLayers: "之后运行的配置不由覆写合成",

	HistReasons: map[string]string{
		"backup-before-apply":  "应用前备份",
		"apply":                "应用",
		"rollback":             "回滚到 %s",
		"adopt":                "接管",
		"adopt-failed":         "接管失败，写回原配置",
		"takeover-undo":        "撤销接管",
		"controller-secure":    "加固控制接口",
		"harden":               "改用 duet 的服务",
		"subscription-updated": "订阅「%s」更新",
		"tailnet-changed":      "Tailscale 设备变化",
		"restore-layers":       "恢复覆写",
	},
	HistBeforeDuet:  "接管前",
	HistCopyHash:    "复制哈希",
	HistOverrides:   "覆写",
	HistNoManifest:  "没有记录",
	HistRollbackRef: "已删除的记录",

	MsgTexts: map[string]string{
		"no-active-subscription": "没有在用的订阅",
		"subscription-empty":     "订阅「%s」还没有内容，先更新它",
		"validate-failed":        "mihomo -t 没有通过，配置没有改动",
		"refuses-exposed":        "%[1]s 对外监听 %[2]s 却没有密钥，duet 不写这样的配置",
		"refuses-socket":         "%[1]s 指向配置目录之外（%[2]s），duet 不写这样的配置",
		"backup-failed":          "没能备份当前配置：%s",
		"write-failed":           "没能写入 %[1]s：%[2]s",
		"reload-failed":          "mihomo 重载失败，已恢复旧配置：%s",
		"verify-failed":          "重载后 mihomo 没有回应，已恢复旧配置：%s",
		"layer-needs-list":       "「%[3]s」后面要写一个列表",
		"layer-verge-list":       "「%[3]s」后面要写一个列表",
		"layer-not-mapping":      "覆写要写成键值（映射）",
	},
	MsgUnknown:     "后台助手返回了错误",
	MsgUnknownWarn: "后台助手返回了一条提示",
	MsgCopyRaw:     "复制原文",

	ImpactTUN: "TUN 会重建（%s），约 1 秒没有网络",
	ImpactTUNWhy: map[string]string{
		"tun":           "tun 设置变了",
		"fake-ip-range": "fake-ip 网段变了",
	},
	ImpactDNS:          "DNS 服务会重新监听，查询会短暂失败",
	ImpactListeners:    "入站端口会重开：%s",
	ImpactProviders:    "经过这些节点集合的连接会断开：%s",
	ImpactSelection:    "策略组会回到第一个节点",
	ImpactUnknown:      "无法比较新旧配置，按最坏情况处理",
	CulpritLayer:       "覆写「%s」用到了 %s",
	CulpritSub:         "订阅原文用到了 %s",
	CulpritOpen:        "打开",
	ApplyFailedRestore: "新配置出错，已恢复旧配置",

	LogsSourceMihomo: "mihomo",
	LogsSourceDuet:   "duet",
	LogSearch:        "搜索",
	LogNoMatch:       "没有匹配的条目",
	EventsEmpty:      "还没有事件",
	EventKinds: map[string]string{
		"compose.applied":      "应用",
		"compose.failed":       "应用",
		"state.changed":        "改动",
		"subscription.updated": "订阅",
		"subscription.failed":  "订阅",
		"tailnet.changed":      "Tailscale",
		"kernel.changed":       "内核",
		"check.changed":        "健康检查",
		"health.updated":       "健康检查",
		"accessory.changed":    "GEO",
		"service.changed":      "服务",
		"takeover.changed":     "接管",
		"helper.started":       "后台助手",
		"helper.stopping":      "后台助手",
	},
	EvApplied:        "已应用 · %s",
	EvApplyUnchanged: "没有变化 · %s",
	EvApplyFailed:    "应用失败 · %s",
	EvState:          "覆写或订阅已修改",
	EvSubUpdated:     "订阅「%s」已更新",
	EvSubSame:        "订阅「%s」没有变化",
	EvSubFailed:      "订阅「%s」更新失败",
	EvTailnet:        "Tailscale 设备有变化",
	EvKernel:         "内核已切换",
	EvAccessory:      "GEO 数据或面板已更新",
	EvCheck:          "%s：%s",
	EvHealth:         "检查完成：%d 项失败，%d 项警告",
	EvService:        "服务状态改变",
	EvTakeover:       "接管计划有变化",
	EvHelperStarted:  "已启动",
	EvHelperStop:     "正在停止",
	EvShow:           "查看",
}

var enConfigUI = ConfigUIStrings{
	GroupSubscription: "Subscription",
	GroupBase:         "Base",
	GroupUser:         "User overrides",
	GroupManaged:      "Managed",

	ManagedTitle:        "Managed by duet",
	OverridesInfo:       "Overrides change the subscription in order, top to bottom; a later one can override an earlier one. Drag a row to reorder.",
	ManagedInfo:         "duet generates these from Tailscale and your settings and keeps them current. You can only turn them on or off. They always come after your own.",
	CoexistInfo:         "Lets mihomo's TUN and Tailscale work together: tailnet addresses and device names go direct, and Tailscale resolves MagicDNS names.",
	LeakGuardInfo:       "Removes DIRECT from the groups you choose, so their traffic never bypasses the proxy. A group left with no other node becomes REJECT.",
	DriftInfo:           "After a rollback mihomo runs a history version, not what these overrides compose. Until you apply again, subscription and Tailscale changes are not applied on their own.",
	ConfigEmptyTitle:    "No subscription yet",
	ConfigEmptyHint:     "Add a subscription link or file, or take over the mihomo config already on this Mac",
	ConfigEmptyTakeover: "Take Over Existing Config",
	StateInSync:         "Matches the running config",
	StateUnknown:        "Couldn't compare with the running config",
	NothingToApply:      "Nothing to apply",
	PreviewHide:         "Hide Preview",
	CoexistRanges:       "Direct ranges",
	CoexistDNS:          "MagicDNS",
	CoexistDevices:      "Devices",
	CoexistDevicesN:     "%d devices",
	CoexistNoTailnet:    "No Tailscale devices read yet",
	GuardGroups:         "Protected groups",
	GuardPick:           "Choose Groups",
	GuardNone:           "No protected groups yet",
	GuardNoGroups:       "No groups chosen",
	GuardMeta:           "Protects %s",
	GuardNoCandidates:   "No group has DIRECT",
	ShowGenerated:       "Show Generated Config",

	SumRule:       "%d rule",
	SumRules:      "%d rules",
	SumProxy:      "%d proxy",
	SumProxies:    "%d proxies",
	SumGroup:      "%d group",
	SumGroups:     "%d groups",
	SumChanges:    "changes %s",
	SumMoreKeys:   " +%d more",
	SumEmpty:      "Empty",
	SubDaily:      "Updates daily",
	SubEveryDays:  "Updates every %d days",
	SubHourly:     "Updates hourly",
	SubEveryHours: "Updates every %d hours",
	SubEveryMins:  "Updates every %d min",

	SubSourceAdopted:     "Source: the config at takeover (%s)",
	SubSourceAdoptedBare: "Source: the config at takeover",
	SubUpdated:           "Updated %s · %s",
	SubCopyHash:          "Copy Hash",

	StageUserLong:    "After the subscription (User)",
	StageBaseLong:    "Base settings before the subscription",
	LayerSaveBlocked: "Fix the lines marked in red first",
	LayerRevert:      "Revert to Applied Version",
	LayerRevertTitle: "Revert “%s” to the applied version?",
	LayerMore:        "More",

	PendingRow:        "Unapplied changes (%d): %s",
	PendingSub:        "Subscription",
	PendingSettings:   "Leak guard",
	PendingRemoved:    "%s (deleted)",
	Discard:           "Discard Changes",
	DiscardTitle:      "Discard unapplied changes?",
	DiscardRemoves:    "Deletes: %s",
	DiscardRestores:   "Reverts: %s",
	PendingUnknownRow: "Unapplied changes",

	ReasonEnable:   "Turn on %s",
	ReasonDisable:  "Turn off %s",
	ReasonEdit:     "Edit %s",
	ReasonEditN:    "Edit %d overrides",
	ReasonOptional: "Optional",
	AppliedAt:      "Applied · %s",
	ViewInHistory:  "View in History",
	ComposeOrder:   "Compose order",

	DriftRow:         "Running: the history version from %s (not composed from the overrides below)",
	DriftRowBackup:   "Running: the backup from %s (not composed from the overrides below)",
	DriftRestore:     "Restore These Overrides",
	DriftReapply:     "Apply Current Overrides",
	DriftRestoreBody: "Only the overrides change; nothing is applied.",
	RollbackDisables: "Turns off: %s",
	RollbackEnables:  "Turns on: %s",
	RollbackChanges:  "Different content: %s",
	RollbackNoLayers: "The overrides will not describe the running config",

	HistReasons: map[string]string{
		"backup-before-apply":  "Backup before apply",
		"apply":                "Apply",
		"rollback":             "Rollback to %s",
		"adopt":                "Takeover",
		"adopt-failed":         "Takeover failed, original config written back",
		"takeover-undo":        "Takeover undone",
		"controller-secure":    "Secure the controller",
		"harden":               "Switch to duet's service",
		"subscription-updated": "Subscription “%s” updated",
		"tailnet-changed":      "Tailscale devices changed",
		"restore-layers":       "Overrides restored",
	},
	HistBeforeDuet:  "Before duet",
	HistCopyHash:    "Copy Hash",
	HistOverrides:   "Overrides",
	HistNoManifest:  "Not recorded",
	HistRollbackRef: "a deleted entry",

	MsgTexts: map[string]string{
		"no-active-subscription": "No subscription is in use",
		"subscription-empty":     "Subscription “%s” has no content yet; update it first",
		"validate-failed":        "mihomo -t failed; the config was not changed",
		"refuses-exposed":        "%[1]s listens on %[2]s beyond this Mac without a secret; duet won't write that",
		"refuses-socket":         "%[1]s points outside the config folder (%[2]s); duet won't write that",
		"backup-failed":          "Couldn't back up the current config: %s",
		"write-failed":           "Couldn't write %[1]s: %[2]s",
		"reload-failed":          "mihomo failed to reload; the old config was restored: %s",
		"verify-failed":          "mihomo didn't answer after the reload; the old config was restored: %s",
		"layer-needs-list":       "“%[3]s” needs a list",
		"layer-verge-list":       "“%[3]s” needs a list",
		"layer-not-mapping":      "An override must be written as keys and values (a mapping)",
	},
	MsgUnknown:     "The helper returned an error",
	MsgUnknownWarn: "The helper returned a notice",
	MsgCopyRaw:     "Copy Original Text",

	ImpactTUN: "TUN is rebuilt (%s), about 1 second without network",
	ImpactTUNWhy: map[string]string{
		"tun":           "tun settings changed",
		"fake-ip-range": "fake-ip range changed",
	},
	ImpactDNS:          "The DNS server rebinds; queries fail for a moment",
	ImpactListeners:    "Inbound ports reopen: %s",
	ImpactProviders:    "Connections through these providers close: %s",
	ImpactSelection:    "Proxy groups go back to their first node",
	ImpactUnknown:      "Couldn't compare the configs; assuming the worst",
	CulpritLayer:       "Override “%s” uses %s",
	CulpritSub:         "The subscription uses %s",
	CulpritOpen:        "Open",
	ApplyFailedRestore: "The new config failed; the old one was restored",

	LogsSourceMihomo: "mihomo",
	LogsSourceDuet:   "duet",
	LogSearch:        "Search",
	LogNoMatch:       "No matching entries",
	EventsEmpty:      "No events yet",
	EventKinds: map[string]string{
		"compose.applied":      "Apply",
		"compose.failed":       "Apply",
		"state.changed":        "Edit",
		"subscription.updated": "Subscription",
		"subscription.failed":  "Subscription",
		"tailnet.changed":      "Tailscale",
		"kernel.changed":       "Core",
		"check.changed":        "Health",
		"health.updated":       "Health",
		"accessory.changed":    "GEO",
		"service.changed":      "Service",
		"takeover.changed":     "Takeover",
		"helper.started":       "Helper",
		"helper.stopping":      "Helper",
	},
	EvApplied:        "Applied · %s",
	EvApplyUnchanged: "No change · %s",
	EvApplyFailed:    "Apply failed · %s",
	EvState:          "Overrides or subscriptions edited",
	EvSubUpdated:     "Subscription “%s” updated",
	EvSubSame:        "Subscription “%s” unchanged",
	EvSubFailed:      "Subscription “%s” failed to update",
	EvTailnet:        "Tailscale devices changed",
	EvKernel:         "Core switched",
	EvAccessory:      "GEO data or dashboard updated",
	EvCheck:          "%s: %s",
	EvHealth:         "Checks done: %d failed, %d warnings",
	EvService:        "Service state changed",
	EvTakeover:       "Takeover plans changed",
	EvHelperStarted:  "Started",
	EvHelperStop:     "Stopping",
	EvShow:           "Show",
}

// Count words n with one (n == 1, with or without a %d) or many (with
// a %d).
func Count(n int, one, many string) string {
	if n == 1 {
		if !strings.Contains(one, "%d") {
			return one
		}
		return F(one, n)
	}
	return F(many, n)
}

// Interval words a subscription's update interval in minutes (> 0) in
// the largest whole unit: 每天, 每 6 小时, 每 30 分钟.
func (s *Strings) Interval(mins int) string {
	switch {
	case mins%1440 == 0:
		return Count(mins/1440, s.SubDaily, s.SubEveryDays)
	case mins%60 == 0:
		return Count(mins/60, s.SubHourly, s.SubEveryHours)
	}
	return F(s.SubEveryMins, mins)
}
