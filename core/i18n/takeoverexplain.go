package i18n

// Texts that explain takeover: the intro on the 接管 page, the hint under
// 生成接管计划, the detected setup before takeover, and the 接管是什么
// section of 关于. Embedded in Strings; fields are promoted (T.TkIntroLead).

// TakeoverExplainStrings holds them.
type TakeoverExplainStrings struct {
	// The 接管 page's intro: one line, three points, the link to 关于.
	TkIntroLead                             string
	TkIntroManage, TkIntroKeep, TkIntroUndo string
	TkLearnMore                             string
	TkPlanNote                              string // under 生成接管计划

	// 现有安装 before takeover: what the running mihomo uses.
	TkNotDetected, TkFoundService, TkFoundBinary string
	KernelStateOwn                               string // 内核 → 状态 before takeover

	// 关于 → 接管是什么.
	AboutTakeover                          string
	AbtTkYourConfig, AbtTkSub, AbtTkLayers string
	AbtTkComposed                          string
	AbtTkChanges                           string
	AbtTkBackup, AbtTkRewrite, AbtTkKeep   string
	AbtTkAutoUndo                          string
	AbtTkEdit, AbtTkEditBody               string
	AbtTkUndo, AbtTkUndoBody               string
	AbtTkWhenNot, AbtTkWhenNotBody         string
}

var zhTakeoverExplain = TakeoverExplainStrings{
	TkIntroLead:   "接管把你的 mihomo 正在用的配置拆成订阅原文和覆写，之后由 duet 维护。",
	TkIntroManage: "订阅自动更新，Tailscale 共存和防泄漏一直生效，每次改动都能回滚",
	TkIntroKeep:   "不重启：mihomo 的服务和程序照旧，网络不断，只重载一次配置",
	TkIntroUndo:   "随时可以撤销接管，换回原来的配置",
	TkLearnMore:   "了解接管",
	TkPlanNote:    "只生成计划，不会改动任何东西",

	TkNotDetected:  "未检测到",
	TkFoundService: "服务",
	TkFoundBinary:  "程序",
	KernelStateOwn: "你自己的程序（未接管）",

	AboutTakeover:    "接管是什么",
	AbtTkYourConfig:  "你手写的 config.yaml",
	AbtTkSub:         "订阅原文",
	AbtTkLayers:      "覆写",
	AbtTkComposed:    "duet 合成的 config.yaml",
	AbtTkChanges:     "接管时会改什么",
	AbtTkBackup:      "先把原来的 config.yaml 原样备份",
	AbtTkRewrite:     "在原位置改写 config.yaml，路径不变；mihomo 重载一次，不重启",
	AbtTkKeep:        "mihomo 的系统服务（plist）和程序都不动",
	AbtTkAutoUndo:    "接管后共存检查变差，自动写回备份",
	AbtTkEdit:        "接管后怎么改配置",
	AbtTkEditBody:    "不要直接改 config.yaml，下次应用时会被覆盖。要改的内容写成覆写。",
	AbtTkUndo:        "怎么撤销",
	AbtTkUndoBody:    "停止使用 duet → 撤销接管：写回接管前的 config.yaml，移除导入的订阅和覆写，服务不变。",
	AbtTkWhenNot:     "什么时候不接管",
	AbtTkWhenNotBody: "想继续手改 config.yaml 时。不接管也能用 duet 查看状态、切换节点、做健康检查。",
}

var enTakeoverExplain = TakeoverExplainStrings{
	TkIntroLead:   "Takeover splits the config your mihomo runs now into the original subscription and overrides, so duet can maintain it.",
	TkIntroManage: "Subscriptions update, Tailscale coexistence and the leak guard stay on, and every change can be rolled back",
	TkIntroKeep:   "No restart: your mihomo service and binary stay, the network keeps running, the config reloads once",
	TkIntroUndo:   "Undo the takeover any time to get your original config back",
	TkLearnMore:   "About Takeover",
	TkPlanNote:    "Only makes a plan; nothing is changed",

	TkNotDetected:  "Not detected",
	TkFoundService: "Service",
	TkFoundBinary:  "Binary",
	KernelStateOwn: "Your own binary (not taken over)",

	AboutTakeover:    "What Is Takeover",
	AbtTkYourConfig:  "your config.yaml",
	AbtTkSub:         "original subscription",
	AbtTkLayers:      "overrides",
	AbtTkComposed:    "config.yaml composed by duet",
	AbtTkChanges:     "What Takeover Changes",
	AbtTkBackup:      "Your config.yaml is backed up as is first",
	AbtTkRewrite:     "config.yaml is rewritten in place, same path; mihomo reloads once, no restart",
	AbtTkKeep:        "mihomo's system service (plist) and binary are left alone",
	AbtTkAutoUndo:    "If the coexistence checks get worse, the backup is written back",
	AbtTkEdit:        "Changing the Config After Takeover",
	AbtTkEditBody:    "Don't edit config.yaml directly: the next apply overwrites it. Put your changes in an override.",
	AbtTkUndo:        "How to Undo",
	AbtTkUndoBody:    "Stop Using duet → Undo Takeover: the config.yaml from before is written back, the imported subscription and overrides are removed, the service stays.",
	AbtTkWhenNot:     "When Not to Take Over",
	AbtTkWhenNotBody: "When you want to keep editing config.yaml by hand. Without takeover duet still shows status, switches nodes and runs health checks.",
}
