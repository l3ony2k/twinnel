package i18n

// Texts of 导入配置: the 导入… entry on 订阅与覆写 and its sheet
// (config.import). Embedded in Strings; fields are promoted (T.ImportRun).

// ImportStrings holds them.
type ImportStrings struct {
	ImportRun   string // button
	ImportTitle string // sheet title
	ImportPick  string // file dialog title
	ImportFiles string // file dialog filter name
	ImportTip   string // ⓘ body after the title
	ImportDo    string // primary button
	ImportDone  string // toast; %d items
	// ImportSource: the kind of file, by config.import's source code.
	ImportSource map[string]string
	// ImportAction: an item's tag, by action code.
	ImportAction map[string]string
	// ImportWarn: a warning, by code.
	ImportWarn         map[string]string
	ImportExisting     string // conflict row: %s the local item's name
	ImportBecomeActive string // %s subscription name
	ImportOff          string // a layer that comes in turned off
	ImportURLOnly      string // a subscription with a URL and no download
}

var zhImport = ImportStrings{
	ImportRun:   "导入…",
	ImportTitle: "导入配置",
	ImportPick:  "选择要导入的文件",
	ImportFiles: "Twinnel 导出、订阅链接或覆写",
	ImportTip: "导入另一台电脑上 Twinnel 导出的配置（停止使用 Twinnel → 导出配置）、一行一个的订阅链接，或 Clash Verge Rev / Clash Party 的覆写文件。" +
		"订阅和覆写的名称、顺序、开关都会保留；Tailscale 共存、防泄漏和接管生成的覆写会在本机重新生成。导入后需要应用才会生效。",
	ImportDo:   "导入",
	ImportDone: "已导入 %d 项",
	ImportSource: map[string]string{
		"twinnel":        "Twinnel 导出",
		"twinnel-legacy": "Twinnel 导出（旧版）",
		"url-list":       "订阅链接列表",
		"override":       "覆写文件",
		"config":         "完整配置，作为订阅导入",
	},
	ImportAction: map[string]string{
		"add":       "新增",
		"unchanged": "已存在",
		"conflict":  "已有同名",
		"generated": "本机重新生成",
		"invalid":   "无法读取",
		"replace":   "替换",
		"skip":      "跳过",
		"rename":    "另存一份",
	},
	ImportWarn: map[string]string{
		"secrets":          "文件里有订阅链接和节点密码，导入后请删除它。",
		"plain_http":       "有订阅链接用的是 http，可能被人截获或替换。",
		"no_content":       "有订阅还没下载，应用前先更新它。",
		"no_subscriptions": "还没有订阅，应用前先添加一个。",
	},
	ImportExisting:     "本机已有「%s」",
	ImportBecomeActive: "「%s」将成为当前订阅",
	ImportOff:          "关闭",
	ImportURLOnly:      "未下载",
}

var enImport = ImportStrings{
	ImportRun:   "Import…",
	ImportTitle: "Import Configuration",
	ImportPick:  "Choose a File to Import",
	ImportFiles: "Twinnel export, subscription links or override",
	ImportTip: "Imports a Twinnel export from another computer (Stop Using Twinnel → Export config), subscription links one per line, or a Clash Verge Rev / Clash Party override file. " +
		"Names, order and on/off state are kept; Tailscale coexistence, leak guard and takeover overrides are rebuilt here. Apply afterwards for it to take effect.",
	ImportDo:   "Import",
	ImportDone: "Imported %d item{s}",
	ImportSource: map[string]string{
		"twinnel":        "Twinnel export",
		"twinnel-legacy": "Twinnel export (older version)",
		"url-list":       "Subscription links",
		"override":       "Override file",
		"config":         "Whole config, imported as a subscription",
	},
	ImportAction: map[string]string{
		"add":       "New",
		"unchanged": "Already here",
		"conflict":  "Name taken",
		"generated": "Rebuilt here",
		"invalid":   "Unreadable",
		"replace":   "Replace",
		"skip":      "Skip",
		"rename":    "Keep both",
	},
	ImportWarn: map[string]string{
		"secrets":          "The file holds subscription links and node passwords. Delete it after importing.",
		"plain_http":       "A subscription link uses http, which others on the network can read or replace.",
		"no_content":       "A subscription is not downloaded yet. Update it before applying.",
		"no_subscriptions": "No subscription yet. Add one before applying.",
	},
	ImportExisting:     "Already here as “%s”",
	ImportBecomeActive: "“%s” becomes the active subscription",
	ImportOff:          "Off",
	ImportURLOnly:      "Not downloaded",
}
