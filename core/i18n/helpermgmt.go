package i18n

// Texts of the 后台助手 section in 设置 (update, restart, log, uninstall,
// remove everything), the version handshake row, and 登录时打开. Embedded
// in Strings; fields are promoted (T.HelperSection).

// HelperMgmtStrings holds them.
type HelperMgmtStrings struct {
	HelperSection   string
	HelperUpdate    string
	HelperRestart   string
	HelperViewLog   string
	HelperUninstall string // opens the confirm dialog
	HelperRemoveAll string // opens the confirm dialog

	// The version handshake: one row, not errors everywhere.
	HelperNeedsUpdate  string
	HelperNewerThanApp string
	HelperVersionVsApp string // %s: helper version, %s: app version

	// The confirm dialogs.
	UninstallTitle, RemoveAllTitle         string
	UninstallRemoves, UninstallKeeps       string // %s: the list
	UninstallPurge                         string // the checkbox
	UninstallConfirm, RemoveAllConfirm     string
	ItemHelper, ItemData, ItemSecrets      string
	ItemDataCounts                         string // %d subscriptions, %d layers
	ItemDuetMihomo                         string // %s: the original service it goes back to
	ItemOriginalMihomo, ItemDuetMihomoKeep string
	ItemTailscale, ItemApp, ItemAppTrash   string
	ItemLoginItem                          string
	UninstallOwnMihomo                     string
	UninstallRefused                       string // %s: the reason

	// Outcomes.
	DoneHelperUpdated, DoneHelperRestarted, DoneHelperUninstalled string

	// 登录时打开.
	General, OpenAtLogin, OpenAtLoginUnsupportedTip, OpenAtLoginApprove string
}

var zhHelperMgmt = HelperMgmtStrings{
	HelperSection:   "后台助手",
	HelperUpdate:    "更新",
	HelperRestart:   "重启",
	HelperViewLog:   "查看日志",
	HelperUninstall: "卸载…",
	HelperRemoveAll: "完全移除 duet…",

	HelperNeedsUpdate:  "需要更新后台助手",
	HelperNewerThanApp: "需要更新 duet",
	HelperVersionVsApp: "%s · app %s",

	UninstallTitle:     "卸载后台助手？",
	RemoveAllTitle:     "完全移除 duet？",
	UninstallRemoves:   "移除：%s",
	UninstallKeeps:     "保留：%s",
	UninstallPurge:     "同时删除数据",
	UninstallConfirm:   "卸载",
	RemoveAllConfirm:   "移除",
	ItemHelper:         "后台助手",
	ItemData:           "数据",
	ItemDataCounts:     "数据（%d 个订阅、%d 个规则层、历史、日志）",
	ItemSecrets:        "钥匙串项目",
	ItemDuetMihomo:     "duet 的 mihomo 服务（切回 %s，断网几秒）",
	ItemOriginalMihomo: "mihomo 原来的服务",
	ItemDuetMihomoKeep: "mihomo 服务",
	ItemTailscale:      "Tailscale",
	ItemApp:            "duet.app",
	ItemAppTrash:       "duet.app（移到废纸篓）",
	ItemLoginItem:      "登录时打开",
	UninstallOwnMihomo: "mihomo 正在用 duet 的数据运行，删除数据会断网。先在「内核」页卸载 mihomo 服务。",
	UninstallRefused:   "无法卸载：%s",

	DoneHelperUpdated:     "后台助手已更新",
	DoneHelperRestarted:   "后台助手已重启",
	DoneHelperUninstalled: "后台助手已卸载",

	General:                   "通用",
	OpenAtLogin:               "登录时打开",
	OpenAtLoginUnsupportedTip: "只有打包后的 app 能登录时打开",
	OpenAtLoginApprove:        "在系统设置 › 通用 › 登录项中允许 duet",
}

var enHelperMgmt = HelperMgmtStrings{
	HelperSection:   "Helper",
	HelperUpdate:    "Update",
	HelperRestart:   "Restart",
	HelperViewLog:   "View Log",
	HelperUninstall: "Uninstall…",
	HelperRemoveAll: "Remove duet Completely…",

	HelperNeedsUpdate:  "Helper needs an update",
	HelperNewerThanApp: "duet needs an update",
	HelperVersionVsApp: "%s · app %s",

	UninstallTitle:     "Uninstall the helper?",
	RemoveAllTitle:     "Remove duet completely?",
	UninstallRemoves:   "Removes: %s",
	UninstallKeeps:     "Keeps: %s",
	UninstallPurge:     "Also delete data",
	UninstallConfirm:   "Uninstall",
	RemoveAllConfirm:   "Remove",
	ItemHelper:         "the helper",
	ItemData:           "data",
	ItemDataCounts:     "data (%d subscriptions, %d overrides, history, logs)",
	ItemSecrets:        "keychain items",
	ItemDuetMihomo:     "duet's mihomo service (back to %s, offline for a few seconds)",
	ItemOriginalMihomo: "mihomo's original service",
	ItemDuetMihomoKeep: "mihomo service",
	ItemTailscale:      "Tailscale",
	ItemApp:            "duet.app",
	ItemAppTrash:       "duet.app (moved to the Trash)",
	ItemLoginItem:      "Open at login",
	UninstallOwnMihomo: "mihomo runs from duet's data; deleting it would cut the network. Uninstall the mihomo service on the Core page first.",
	UninstallRefused:   "Can't uninstall: %s",

	DoneHelperUpdated:     "Helper updated",
	DoneHelperRestarted:   "Helper restarted",
	DoneHelperUninstalled: "Helper uninstalled",

	General:                   "General",
	OpenAtLogin:               "Open at login",
	OpenAtLoginUnsupportedTip: "Only the packaged app can open at login",
	OpenAtLoginApprove:        "Allow duet in System Settings › General › Login Items",
}
