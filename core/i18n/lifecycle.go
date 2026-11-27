package i18n

// Texts of batch A (trust and lifecycle): the takeover plan's 接管后
// table, the base file, the adopt result and its 下一步, the helper
// states, the install page's load states, the service and kernel cards,
// and the 停止使用 duet page (what duet installed, 撤销接管, 卸载, 完全移除,
// 导出配置). Embedded in Strings; fields are promoted (T.LeavePage).

// LifecycleStrings holds them.
type LifecycleStrings struct {
	// --- Takeover: what exists now and what happens to it. ---
	TkNow, TkAfter                                   string // column heads
	TkSecretItem                                     string
	TkEffKeepService                                 string
	TkEffNoService                                   string
	TkEffKeep                                        string
	TkEffReplaceable                                 string
	TkEffReplaceableTip                              string // %s: the binary's path
	TkEffRewrite                                     string
	TkEffStoreSub                                    string
	TkEffSkipSub                                     string // %d profiles
	TkEffNoSub                                       string
	TkEffKeepSecret                                  string
	TkEffNoSecret                                    string
	TkEffUnchanged                                   string
	TkEditsLost                                      string // %s: the config file name
	TkNewOverride                                    string
	TkRewriteCount                                   string // %d: rows duet rewrites (the 接管 button row)
	TkAdoptConfirmBody                               string // %s: the rewrite rows, one per line
	TkLiveRunning                                    string // the target row: %s file name
	TkTarget                                         string
	TkExplains                                       string // verdict chip
	TkIncomplete                                     string
	TkCandDetail                                     string // disclosure
	TkPickFile                                       string
	TkSubURL                                         string // row label
	TkSubURLFoundNow                                 string
	TkSubURLNone                                     string
	TkOtherCandidates                                string // %d
	TkCopyPlanID                                     string
	TkDiscard                                        string // context menu item
	TkFoundLive                                      string // the empty state's rows
	TkFoundNotRunning                                string
	TkCopyDetails                                    string
	TkMoreN                                          string // %d more
	TkShowKeys                                       string // %d entries
	TkHideKeys                                       string
	TkSampleSingleLabel                              string // %s: resolver
	TkSampleArrow                                    string // %s key → %s resolver
	TkSampleFakeIP                                   string // %s: names
	TkSampleHosts                                    string // %s: names
	TkImpactTUN                                      string
	TkImpactDNS                                      string
	TkImpactListeners                                string // %s: names
	TkImpactProviders                                string // %s: names
	TkImpactSelection                                string
	TkImpactRestart                                  string // %s: settings
	TkStepNames                                      map[string]string
	TkStepOK                                         map[string]string // step code → short phrase on success
	TkStepFailed                                     string
	TkStepSkipped                                    string
	TkDoneNeedsWork                                  string // %d checks still failing
	TkCheckOK, TkCheckWarn, TkCheckFail, TkCheckSkip string
	TkCheckBetter                                    string // tooltip on ↑
	TkCheckWorse                                     string // tooltip on ↓
	TkNext                                           string
	TkNextChecks                                     string // %d
	TkNextSecure                                     string
	TkNextKernel                                     string
	TkNextUndo                                       string
	TkSecretState                                    string // row label
	TkSecretSetWeak                                  string
	// The takeover result's 控制接口密钥 line.
	TkResSecretKept, TkResSecretNew, TkResSecretNone string
	TkSecretNone                                     string

	// --- Helper states (U6, U7). ---
	HelperStateNotRunning string
	HelperRestartAdmin    string // button
	HelperCopyDiag        string

	// --- Install page (U2). ---
	InstallPlanFailed, InstallPlanTimeout, InstallRetry string
	InstallNeedsWrites                                  string // lock tooltip

	// --- Services (U44, U55). ---
	SvcAdopted    string // chip
	SvcMore       string // ⋯ menu label
	SvcOwnRunning string // mihomo runs, not from a service duet knows

	// Keychain items in words (停止使用 duet). %s: subscription ID.
	SecretController, SecretSubURL, SecretSubAdopted string
	SvcAdoptedTip                                    string

	// --- Kernel (U43, H11, U58). ---
	KernelStateExternal, KernelStateManaged string
	KernelPendingRestart                    string
	KernelCheckFailed                       string
	KernelUpdateBody                        string // %s current, %s new
	KernelFirstBody                         string // %s new

	// --- About (U10, U11). ---
	FAQLeaveQ, FAQLeaveA                    string
	AppPrefs, MihomoConfigRow, ShowInFinder string

	// --- 停止使用 duet (U54, U55, U56). ---
	LeavePage                              string
	LeaveInstalled                         string
	LeaveInventoryLoading                  string
	InvHelperBinary, InvHelperService      string
	InvHelperSocket, InvStateDir           string
	InvSubscriptions, InvLayers            string
	InvHistory, InvTakeoverBackups         string
	InvKernels, InvLogs, InvSecrets        string
	InvDuetMihomo, InvAdoptedMihomo        string
	InvApp, InvAppPrefs, InvAppBackups     string
	InvLaunchAgent, InvLoginItem           string
	InvCount                               string // %d items
	LeaveUndo, LeaveUndoTitle              string
	LeaveUndoRun                           string
	LeaveUndoNotAdopted, LeaveUndoHardened string
	UndoSteps                              map[string]string // code → line (%s path / %d count where used)
	UndoDone, UndoFailed                   string
	LeaveUninstall, LeaveUninstallRun      string
	LeaveUninstallWhat, LeaveRemoveAllWhat string // each card's row: what it does
	LeaveRemoveAll, LeaveRemoveAllRun      string
	LeaveExport, LeaveExportRun            string
	LeaveExportDone                        string // %s: file
	LeaveExportWhat                        string // %d: overrides
	ItemAppBackups, ItemLaunchAgent        string
}

var zhLifecycle = LifecycleStrings{
	TkNow:               "现在",
	TkAfter:             "接管后",
	TkSecretItem:        "控制接口密钥",
	TkEffKeepService:    "保留 · 不改 plist，由 duet 启停",
	TkEffNoService:      "没有服务",
	TkEffKeep:           "保留",
	TkEffReplaceable:    "保留 · 普通用户可替换",
	TkEffReplaceableTip: "%s 不归 root 所有",
	TkEffRewrite:        "原位置改写 · 先备份",
	TkEffStoreSub:       "存入钥匙串 · 首次手动更新时下载",
	TkEffSkipSub:        "找到 %d 个 · 不导入",
	TkEffNoSub:          "未找到 · 以订阅原文为准",
	TkEffKeepSecret:     "保留 · 可稍后加固",
	TkEffNoSecret:       "未设 · 可稍后加固",
	TkEffUnchanged:      "不变",
	TkEditsLost:         "此后直接修改 %s 会在下次应用时被覆盖",
	TkNewOverride:       "新建覆写",
	TkRewriteCount:      "改写 %d 项",
	TkAdoptConfirmBody:  "%s\n健康检查变差会自动恢复。",
	TkLiveRunning:       "%s（运行中）",
	TkTarget:            "目标",
	TkExplains:          "能还原运行中的配置",
	TkIncomplete:        "不完整",
	TkCandDetail:        "对比明细",
	TkPickFile:          "换一个文件…",
	TkSubURL:            "订阅地址",
	TkSubURLFoundNow:    "已找到",
	TkSubURLNone:        "未找到",
	TkOtherCandidates:   "其他候选 %d 个",
	TkCopyPlanID:        "复制计划 ID",
	TkDiscard:           "删除计划…",
	TkFoundLive:         "运行中",
	TkFoundNotRunning:   "未运行",
	TkCopyDetails:       "复制详情",
	TkMoreN:             "另有 %d 条",
	TkShowKeys:          "查看 %d 项",
	TkHideKeys:          "收起",
	TkSampleSingleLabel: "单标签名（如 nas、laptop）→ %s",
	TkSampleArrow:       "%s → %s",
	TkSampleFakeIP:      "拿真实地址：%s",
	TkSampleHosts:       "本地解析：%s",
	TkImpactTUN:         "TUN 重建，约 1 秒断网",
	TkImpactDNS:         "DNS 重新监听",
	TkImpactListeners:   "入站端口重建：%s",
	TkImpactProviders:   "经这些订阅节点的连接会断开：%s",
	TkImpactSelection:   "策略组回到第一个节点",
	TkImpactRestart:     "重启 mihomo 后生效：%s",
	TkStepNames: map[string]string{
		"verify":        "核对",
		"backup":        "备份",
		"import":        "导入",
		"health-before": "检查（前）",
		"apply":         "应用",
		"health-after":  "检查（后）",
		"rollback":      "回滚",
		"restore":       "恢复",
		"undo-import":   "撤销导入",
	},
	TkStepOK: map[string]string{
		"verify":        "配置自生成计划后没有变",
		"backup":        "原配置已备份",
		"import":        "订阅原文和覆写已导入",
		"health-before": "已记录",
		"apply":         "已写入并重载",
		"health-after":  "没有变差",
		"rollback":      "原配置已写回并重载",
		"restore":       "原配置已写回",
		"undo-import":   "导入已撤销",
	},
	TkStepFailed:    "失败",
	TkStepSkipped:   "跳过",
	TkDoneNeedsWork: "接管完成 · %d 项仍需处理",
	TkCheckOK:       "通过",
	TkCheckWarn:     "警告",
	TkCheckFail:     "失败",
	TkCheckSkip:     "跳过",
	TkCheckBetter:   "接管后变好",
	TkCheckWorse:    "接管后变差",
	TkNext:          "下一步",
	TkNextChecks:    "处理剩余检查（%d）",
	TkNextSecure:    "加固控制接口",
	TkNextKernel:    "改用 duet 管理的内核",
	TkNextUndo:      "撤销接管…",
	TkSecretState:   "当前密钥",
	TkSecretSetWeak: "已设",
	TkResSecretKept: "沿用你原来的密钥",
	TkResSecretNew:  "已生成新密钥",
	TkResSecretNone: "未设，可在下面加固",
	TkSecretNone:    "未设",

	HelperStateNotRunning: "后台助手未运行",
	HelperRestartAdmin:    "重新启动",
	HelperCopyDiag:        "复制诊断信息",

	InstallPlanFailed:  "无法预览安装步骤",
	InstallPlanTimeout: "预览安装步骤超时",
	InstallRetry:       "重试",
	InstallNeedsWrites: "开发运行：只读",

	SvcAdopted:    "接管",
	SvcMore:       "更多",
	SvcOwnRunning: "运行中 · 由你自己的服务运行",

	SecretController: "控制接口密钥",
	SecretSubURL:     "订阅「%s」的地址",
	SecretSubAdopted: "接管时导入的订阅地址",
	SvcAdoptedTip:    "接管来的服务：duet 只启停，不改它的定义",

	KernelStateExternal:  "外部程序（接管）",
	KernelStateManaged:   "duet 管理",
	KernelPendingRestart: "待重启",
	KernelCheckFailed:    "检查失败",
	KernelUpdateBody:     "%s → %s · 需重启 mihomo（断网约 1 秒）\n上一个版本保留，可以回滚。",
	KernelFirstBody:      "外部程序 → %s · 需重启 mihomo（断网约 1 秒）\n外部程序保留，可以改回。",

	FAQLeaveQ:       "怎么彻底停用 duet？",
	FAQLeaveA:       "设置 → 停止使用 duet：撤销接管、卸载后台助手，或完全移除。",
	AppPrefs:        "duet 偏好设置",
	MihomoConfigRow: "mihomo 配置",
	ShowInFinder:    "在 Finder 中显示",

	LeavePage:             "停止使用 duet",
	LeaveInstalled:        "duet 在本机安装的内容",
	LeaveInventoryLoading: "读取中…",
	InvHelperBinary:       "后台助手程序",
	InvHelperService:      "后台助手服务（LaunchDaemon）",
	InvHelperSocket:       "后台助手接口",
	InvStateDir:           "duet 数据",
	InvSubscriptions:      "订阅",
	InvLayers:             "覆写",
	InvHistory:            "历史",
	InvTakeoverBackups:    "接管计划与备份",
	InvKernels:            "内核",
	InvLogs:               "日志",
	InvSecrets:            "钥匙串项目",
	InvDuetMihomo:         "duet 的 mihomo 服务",
	InvAdoptedMihomo:      "你的 mihomo 服务（保留）",
	InvApp:                "duet App",
	InvAppPrefs:           "duet 偏好设置",
	InvAppBackups:         "旧版本备份",
	InvLaunchAgent:        "登录项（LaunchAgent）",
	InvLoginItem:          "登录时打开",
	InvCount:              "%d 项",
	LeaveUndo:             "撤销接管",
	LeaveUndoTitle:        "撤销接管？",
	LeaveUndoRun:          "撤销接管",
	LeaveUndoNotAdopted:   "没有接管",
	LeaveUndoHardened:     "mihomo 已改用 duet 的服务：卸载后台助手时一并改回",
	UndoSteps: map[string]string{
		"restore-config":      "写回接管前的 %s",
		"reload":              "mihomo 重载，继续运行",
		"restore-secret":      "控制接口密钥改回接管前的",
		"remove-subscription": "移除导入的订阅",
		"remove-layers":       "移除导入的 %d 个覆写",
		"disable-layers":      "关闭之后新建的 %d 个覆写（保留）",
		"keep-service":        "服务 %s 不变",
		"keep-data":           "保留历史和接管备份，可以再次接管",
	},
	UndoDone:           "已撤销接管",
	UndoFailed:         "撤销接管失败",
	LeaveUninstall:     "卸载后台助手",
	LeaveUninstallWhat: "订阅不再自动更新，duet 不能再改配置",
	LeaveRemoveAllWhat: "删除 duet 的数据和钥匙串项目",
	LeaveUninstallRun:  "卸载…",
	LeaveRemoveAll:     "完全移除 duet",
	LeaveRemoveAllRun:  "完全移除…",
	LeaveExport:        "导出配置",
	LeaveExportRun:     "导出…",
	LeaveExportDone:    "已导出到 %s",
	LeaveExportWhat:    "config.yaml · 覆写 %d 个",
	ItemAppBackups:     "旧版本备份",
	ItemLaunchAgent:    "duet 的 LaunchAgent",
}

var enLifecycle = LifecycleStrings{
	TkNow:               "Now",
	TkAfter:             "After takeover",
	TkSecretItem:        "Controller secret",
	TkEffKeepService:    "Kept · plist unchanged, started and stopped by duet",
	TkEffNoService:      "No service",
	TkEffKeep:           "Kept",
	TkEffReplaceable:    "Kept · any user can replace it",
	TkEffReplaceableTip: "%s isn't owned by root",
	TkEffRewrite:        "Rewritten in place · backed up first",
	TkEffStoreSub:       "Saved to Keychain · downloaded on the first manual update",
	TkEffSkipSub:        "%d found · not imported",
	TkEffNoSub:          "None found · the source file is used",
	TkEffKeepSecret:     "Kept · harden later",
	TkEffNoSecret:       "Not set · harden later",
	TkEffUnchanged:      "Unchanged",
	TkEditsLost:         "After takeover, direct edits to %s are overwritten on the next apply",
	TkNewOverride:       "New Override",
	TkRewriteCount:      "Rewrites %d",
	TkAdoptConfirmBody:  "%s\nIf health checks get worse, duet restores the old config.",
	TkLiveRunning:       "%s (running)",
	TkTarget:            "Target",
	TkExplains:          "Rebuilds the running config",
	TkIncomplete:        "Incomplete",
	TkCandDetail:        "Details",
	TkPickFile:          "Choose Another File…",
	TkSubURL:            "Subscription URL",
	TkSubURLFoundNow:    "Found",
	TkSubURLNone:        "Not found",
	TkOtherCandidates:   "Other candidates: %d",
	TkCopyPlanID:        "Copy Plan ID",
	TkDiscard:           "Delete Plan…",
	TkFoundLive:         "Running",
	TkFoundNotRunning:   "Not running",
	TkCopyDetails:       "Copy Details",
	TkMoreN:             "More (%d)",
	TkShowKeys:          "Show All (%d)",
	TkHideKeys:          "Hide",
	TkSampleSingleLabel: "Single-label names (nas, laptop) → %s",
	TkSampleArrow:       "%s → %s",
	TkSampleFakeIP:      "Real addresses for: %s",
	TkSampleHosts:       "Answered locally: %s",
	TkImpactTUN:         "TUN is recreated, about 1 s offline",
	TkImpactDNS:         "DNS listener restarts",
	TkImpactListeners:   "Inbound listeners recreated: %s",
	TkImpactProviders:   "Connections through these providers are reset: %s",
	TkImpactSelection:   "Proxy groups go back to their first node",
	TkImpactRestart:     "Takes effect after mihomo restarts: %s",
	TkStepNames: map[string]string{
		"verify":        "Verify",
		"backup":        "Back up",
		"import":        "Import",
		"health-before": "Check (before)",
		"apply":         "Apply",
		"health-after":  "Check (after)",
		"rollback":      "Roll back",
		"restore":       "Restore",
		"undo-import":   "Undo import",
	},
	TkStepOK: map[string]string{
		"verify":        "Config unchanged since the plan",
		"backup":        "Old config backed up",
		"import":        "Source and overrides imported",
		"health-before": "Recorded",
		"apply":         "Written and reloaded",
		"health-after":  "Nothing got worse",
		"rollback":      "Old config written back and reloaded",
		"restore":       "Old config written back",
		"undo-import":   "Import undone",
	},
	TkStepFailed:    "Failed",
	TkStepSkipped:   "Skipped",
	TkDoneNeedsWork: "Takeover done · %d need attention",
	TkCheckOK:       "Passed",
	TkCheckWarn:     "Warning",
	TkCheckFail:     "Failed",
	TkCheckSkip:     "Skipped",
	TkCheckBetter:   "Better after takeover",
	TkCheckWorse:    "Worse after takeover",
	TkNext:          "Next",
	TkNextChecks:    "Fix Remaining Checks (%d)",
	TkNextSecure:    "Harden Controller",
	TkNextKernel:    "Use duet's Core",
	TkNextUndo:      "Undo Takeover…",
	TkSecretState:   "Current secret",
	TkSecretSetWeak: "Set",
	TkResSecretKept: "Kept your existing secret",
	TkResSecretNew:  "A new secret was generated",
	TkResSecretNone: "Not set; harden it below",
	TkSecretNone:    "None",

	HelperStateNotRunning: "Helper not running",
	HelperRestartAdmin:    "Start Again",
	HelperCopyDiag:        "Copy Diagnostics",

	InstallPlanFailed:  "Can't preview the install steps",
	InstallPlanTimeout: "Previewing the install steps timed out",
	InstallRetry:       "Try Again",
	InstallNeedsWrites: "Development run: read-only",

	SvcAdopted:    "Adopted",
	SvcMore:       "More",
	SvcOwnRunning: "Running · from your own service",

	SecretController: "Controller secret",
	SecretSubURL:     "URL of subscription “%s”",
	SecretSubAdopted: "Subscription URL from takeover",
	SvcAdoptedTip:    "Adopted service: duet starts and stops it but never changes its definition",

	KernelStateExternal:  "External binary (adopted)",
	KernelStateManaged:   "Managed by duet",
	KernelPendingRestart: "Restart pending",
	KernelCheckFailed:    "Check failed",
	KernelUpdateBody:     "%s → %s · mihomo restarts (about 1 s offline)\nThe previous version is kept for rollback.",
	KernelFirstBody:      "External binary → %s · mihomo restarts (about 1 s offline)\nThe external binary is kept; you can switch back.",

	FAQLeaveQ:       "How do I stop using duet completely?",
	FAQLeaveA:       "Settings → Stop Using duet: undo the takeover, uninstall the helper, or remove everything.",
	AppPrefs:        "duet preferences",
	MihomoConfigRow: "mihomo config",
	ShowInFinder:    "Show in Finder",

	LeavePage:             "Stop Using duet",
	LeaveInstalled:        "What duet Installed on This Mac",
	LeaveInventoryLoading: "Reading…",
	InvHelperBinary:       "Helper binary",
	InvHelperService:      "Helper service (LaunchDaemon)",
	InvHelperSocket:       "Helper socket",
	InvStateDir:           "duet data",
	InvSubscriptions:      "Subscriptions",
	InvLayers:             "Overrides",
	InvHistory:            "History",
	InvTakeoverBackups:    "Takeover plans and backups",
	InvKernels:            "Cores",
	InvLogs:               "Logs",
	InvSecrets:            "Keychain items",
	InvDuetMihomo:         "duet's mihomo service",
	InvAdoptedMihomo:      "Your mihomo service (kept)",
	InvApp:                "duet app",
	InvAppPrefs:           "duet preferences",
	InvAppBackups:         "Old version backups",
	InvLaunchAgent:        "Login item (LaunchAgent)",
	InvLoginItem:          "Open at login",
	InvCount:              "Items: %d",
	LeaveUndo:             "Undo takeover",
	LeaveUndoTitle:        "Undo the takeover?",
	LeaveUndoRun:          "Undo Takeover",
	LeaveUndoNotAdopted:   "Not taken over",
	LeaveUndoHardened:     "mihomo runs from duet's service: uninstalling the helper moves it back",
	UndoSteps: map[string]string{
		"restore-config":      "Write back %s as it was before takeover",
		"reload":              "mihomo reloads and keeps running",
		"restore-secret":      "Controller secret goes back to the old one",
		"remove-subscription": "Remove the imported subscription",
		"remove-layers":       "Remove the %d imported override{s}",
		"disable-layers":      "Turn off %d override{s} made later (kept)",
		"keep-service":        "Service %s is not changed",
		"keep-data":           "History and takeover backups are kept, so you can take over again",
	},
	UndoDone:           "Takeover undone",
	UndoFailed:         "Undoing the takeover failed",
	LeaveUninstall:     "Uninstall the helper",
	LeaveUninstallWhat: "Subscriptions stop updating and duet can't change the config",
	LeaveRemoveAllWhat: "Deletes duet's data and keychain items",
	LeaveUninstallRun:  "Uninstall…",
	LeaveRemoveAll:     "Remove duet completely",
	LeaveRemoveAllRun:  "Remove…",
	LeaveExport:        "Export config",
	LeaveExportRun:     "Export…",
	LeaveExportDone:    "Exported to %s",
	LeaveExportWhat:    "config.yaml · overrides: %d",
	ItemAppBackups:     "old version backups",
	ItemLaunchAgent:    "duet's LaunchAgent",
}
