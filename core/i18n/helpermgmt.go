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
	UninstallTitle, RemoveAllTitle            string
	UninstallRemoves, UninstallKeeps          string // %s: the list
	UninstallPurge                            string // the checkbox
	UninstallConfirm, RemoveAllConfirm        string
	ItemHelper, ItemData, ItemSecrets         string
	ItemDataCounts                            string // %d subscriptions, %d layers
	ItemTwinnelMihomo                         string // %s: the original service it goes back to
	ItemOriginalMihomo, ItemTwinnelMihomoKeep string
	ItemTailscale, ItemApp, ItemAppTrash      string
	ItemLoginItem                             string
	ItemSystemDNS                             string // the system DNS Twinnel set, written back
	UninstallOwnMihomo                        string
	UninstallRefused                          string // %s: the reason

	// Outcomes.
	DoneHelperUpdated, DoneHelperRestarted, DoneHelperUninstalled string

	// 登录时打开.
	General, OpenAtLogin, OpenAtLoginUnsupportedTip, OpenAtLoginApprove string

	// 在程序坞中显示 (macOS): when Twinnel has a Dock icon.
	ShowInDock, DockWhenOpen, DockAlways, DockNever string
	DockOpen                                        string // the Dock menu's item that opens the main window

	// The helper's socket is for a group this session is not in (Linux).
	// %s: the group. Missing: the user is not in it (加入 adds them);
	// Relogin: they were added and must log in again. Row*: the sidebar's
	// and the panel's short forms.
	HelperGroupMissing, HelperGroupRelogin, HelperGroupFix string
	HelperGroupRowMissing, HelperGroupRowRelogin           string
	DoneHelperGroup                                        string
}

var zhHelperMgmt = HelperMgmtStrings{
	HelperSection:   "后台助手",
	HelperUpdate:    "更新",
	HelperRestart:   "重启",
	HelperViewLog:   "查看日志",
	HelperUninstall: "卸载…",
	HelperRemoveAll: "完全移除 Twinnel…",

	HelperNeedsUpdate:  "需要更新后台助手",
	HelperNewerThanApp: "需要更新 Twinnel",
	HelperVersionVsApp: "%s · app %s",

	UninstallTitle:        "卸载后台助手？",
	RemoveAllTitle:        "完全移除 Twinnel？",
	UninstallRemoves:      "移除：%s",
	UninstallKeeps:        "保留：%s",
	UninstallPurge:        "同时删除数据",
	UninstallConfirm:      "卸载",
	RemoveAllConfirm:      "移除",
	ItemHelper:            "后台助手",
	ItemData:              "数据",
	ItemDataCounts:        "数据（%d 个订阅、%d 个覆写、历史、日志）",
	ItemSecrets:           "钥匙串项目",
	ItemTwinnelMihomo:     "Twinnel 的 mihomo 服务（切回 %s，断网几秒）",
	ItemOriginalMihomo:    "mihomo 原来的服务",
	ItemTwinnelMihomoKeep: "mihomo 服务",
	ItemTailscale:         "Tailscale",
	ItemApp:               "Twinnel.app",
	ItemAppTrash:          "Twinnel.app（移到废纸篓）",
	ItemLoginItem:         "登录时打开",
	ItemSystemDNS:         "系统 DNS 设置（改回原来的）",
	UninstallOwnMihomo:    "mihomo 正在用 Twinnel 的数据运行，删除数据会断网。先在「内核」页卸载 mihomo 服务。",
	UninstallRefused:      "无法卸载：%s",

	DoneHelperUpdated:     "后台助手已更新",
	DoneHelperRestarted:   "后台助手已重启",
	DoneHelperUninstalled: "后台助手已卸载",

	General:                   "通用",
	OpenAtLogin:               "登录时打开",
	OpenAtLoginUnsupportedTip: "只有打包后的 app 能登录时打开",
	OpenAtLoginApprove:        "在系统设置 › 通用 › 登录项中允许 Twinnel",
	HelperGroupMissing:        "你的用户不在「%s」组，连不上后台助手",
	HelperGroupRelogin:        "已加入「%s」组：注销并重新登录后生效",
	HelperGroupFix:            "加入",
	HelperGroupRowMissing:     "后台助手 · 需要加入 %s 组",
	HelperGroupRowRelogin:     "后台助手 · 重新登录后可用",
	DoneHelperGroup:           "已加入组：注销并重新登录后生效",

	ShowInDock:   "在程序坞中显示",
	DockWhenOpen: "打开窗口时",
	DockAlways:   "始终",
	DockNever:    "从不",
	DockOpen:     "打开 Twinnel",
}

var enHelperMgmt = HelperMgmtStrings{
	HelperSection:   "Helper",
	HelperUpdate:    "Update",
	HelperRestart:   "Restart",
	HelperViewLog:   "View Log",
	HelperUninstall: "Uninstall…",
	HelperRemoveAll: "Remove Twinnel Completely…",

	HelperNeedsUpdate:  "Helper outdated",
	HelperNewerThanApp: "Twinnel needs an update",
	HelperVersionVsApp: "%s · app %s",

	UninstallTitle:        "Uninstall the helper?",
	RemoveAllTitle:        "Remove Twinnel completely?",
	UninstallRemoves:      "Removes: %s",
	UninstallKeeps:        "Keeps: %s",
	UninstallPurge:        "Also delete data",
	UninstallConfirm:      "Uninstall",
	RemoveAllConfirm:      "Remove",
	ItemHelper:            "the helper",
	ItemData:              "data",
	ItemDataCounts:        "data (%d subscription{s}, %d override{s}, history, logs)",
	ItemSecrets:           "keychain items",
	ItemTwinnelMihomo:     "Twinnel's mihomo service (back to %s, offline for a few seconds)",
	ItemOriginalMihomo:    "mihomo's original service",
	ItemTwinnelMihomoKeep: "mihomo service",
	ItemTailscale:         "Tailscale",
	ItemApp:               "Twinnel.app",
	ItemAppTrash:          "Twinnel.app (moved to the Trash)",
	ItemLoginItem:         "Open at login",
	ItemSystemDNS:         "the system DNS setting (back to what it was)",
	UninstallOwnMihomo:    "mihomo runs from Twinnel's data; deleting it would cut the network. Uninstall the mihomo service on the Core page first.",
	UninstallRefused:      "Can't uninstall: %s",

	DoneHelperUpdated:     "Helper updated",
	DoneHelperRestarted:   "Helper restarted",
	DoneHelperUninstalled: "Helper uninstalled",

	General:                   "General",
	OpenAtLogin:               "Open at login",
	OpenAtLoginUnsupportedTip: "Only the packaged app can open at login",
	OpenAtLoginApprove:        "Allow Twinnel in System Settings › General › Login Items",
	HelperGroupMissing:        "Your user isn't in the %s group, so it can't reach the helper",
	HelperGroupRelogin:        "Added to the %s group: log out and back in",
	HelperGroupFix:            "Add Me",
	HelperGroupRowMissing:     "Helper · needs the %s group",
	HelperGroupRowRelogin:     "Helper · log in again to connect",
	DoneHelperGroup:           "Added to the group: log out and back in",

	ShowInDock:   "Show in Dock",
	DockWhenOpen: "While the window is open",
	DockAlways:   "Always",
	DockNever:    "Never",
	DockOpen:     "Open Twinnel",
}
