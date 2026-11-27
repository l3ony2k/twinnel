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
	TkPlanRunning                           string // next to 生成接管计划 while the plan is made
	TkPlanElapsed                           string // seconds since it started (%d)

	// 现有安装 before takeover: what the running mihomo uses.
	TkNotDetected, TkFoundService, TkFoundBinary string
	KernelStateOwn                               string // 内核 → 状态 before takeover

	// ⓘ on the 接管 page's cards.
	TipFound, TipEffects, TipBase, TipLayers  string
	TipVerdict, TipAdopt, TipSecure, TipPlans string

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
	TkIntroLead:   "接管把你的 mihomo 正在用的配置拆成订阅原文和覆写，之后由 Twinnel 维护。",
	TkIntroManage: "订阅自动更新，Tailscale 共存和防泄漏一直生效，每次改动都能回滚",
	TkIntroKeep:   "不重启：mihomo 的服务和程序照旧，只重载一次配置；对网络的影响写在计划里",
	TkIntroUndo:   "随时可以撤销接管，换回原来的配置",
	TkLearnMore:   "了解接管",
	TkPlanRunning: "正在比对现有配置和订阅…",
	TkPlanElapsed: "%d 秒",

	TkNotDetected:  "未检测到",
	TkFoundService: "服务",
	TkFoundBinary:  "程序",
	KernelStateOwn: "你自己的程序（未接管）",

	TipFound:   "mihomo 的系统服务正在用的程序和配置文件。接管只改写这个配置文件；生成接管计划不会改动任何东西。",
	TipEffects: "每一项现在是什么、接管后会怎样。标「保留」的不会被改动。",
	TipBase:    "你的配置最初来自的订阅文件。Twinnel 用它加上覆写，重新合成出和现在一样的配置。",
	TipLayers:  "订阅原文之外、你自己加的改动，接管后照样生效。标「托管」的由 Twinnel 生成和维护，例如 Tailscale 共存。",
	TipVerdict: "把 Twinnel 合成的配置和正在运行的配置逐项对比。「有意的修改」是 Twinnel 为 Tailscale 共存加的。",
	TipAdopt:   "备份 config.yaml，导入订阅原文和覆写，在原位置写入并重载 mihomo。共存检查变差时自动写回备份。",
	TipSecure:  "给 mihomo 的控制接口设一个密钥，其他程序就不能随意切换节点或改配置。Twinnel 会自动使用它。",
	TipPlans:   "计划只记下接管会做什么，不改动任何东西。点「接管」之前都可以删掉重做。",

	AboutTakeover:    "接管是什么",
	AbtTkYourConfig:  "你手写的 config.yaml",
	AbtTkSub:         "订阅原文",
	AbtTkLayers:      "覆写",
	AbtTkComposed:    "Twinnel 合成的 config.yaml",
	AbtTkChanges:     "接管时会改什么",
	AbtTkBackup:      "先把原来的 config.yaml 原样备份",
	AbtTkRewrite:     "在原位置改写 config.yaml，路径不变；mihomo 重载一次，不重启",
	AbtTkKeep:        "mihomo 的系统服务（plist）和程序都不动",
	AbtTkAutoUndo:    "接管后共存检查变差，自动写回备份",
	AbtTkEdit:        "接管后怎么改配置",
	AbtTkEditBody:    "不要直接改 config.yaml，下次应用时会被覆盖。要改的内容写成覆写。",
	AbtTkUndo:        "怎么撤销",
	AbtTkUndoBody:    "停止使用 Twinnel → 撤销接管：写回接管前的 config.yaml，移除导入的订阅和覆写，服务不变。",
	AbtTkWhenNot:     "什么时候不接管",
	AbtTkWhenNotBody: "想继续手改 config.yaml 时。不接管也能用 Twinnel 查看状态、切换节点、做健康检查。",
}

var enTakeoverExplain = TakeoverExplainStrings{
	TkIntroLead:   "Takeover splits the config your mihomo runs now into the original subscription and overrides, so Twinnel can maintain it.",
	TkIntroManage: "Subscriptions update, Tailscale coexistence and the leak guard stay on, and every change can be rolled back",
	TkIntroKeep:   "No restart: your mihomo service and binary stay and the config reloads once; the plan shows any effect on the network",
	TkIntroUndo:   "Undo the takeover any time to get your original config back",
	TkLearnMore:   "About Takeover",
	TkPlanRunning: "Comparing your config with the subscription…",
	TkPlanElapsed: "%ds",

	TkNotDetected:  "Not detected",
	TkFoundService: "Service",
	TkFoundBinary:  "Binary",
	KernelStateOwn: "Your own binary (not taken over)",

	TipFound:   "The binary and config file your mihomo service runs. Takeover rewrites only this config file; making a plan changes nothing.",
	TipEffects: "What each piece is now and what happens to it after takeover. Items marked Kept are not changed.",
	TipBase:    "The subscription file your config was built from. twinnel composes it with the overrides to rebuild the config you run now.",
	TipLayers:  "Your own changes on top of the original subscription; they keep working after takeover. Managed ones are generated and kept up by Twinnel, such as Tailscale coexistence.",
	TipVerdict: "The config twinnel composes, compared with the running one item by item. Deliberate changes are what Twinnel adds for Tailscale coexistence.",
	TipAdopt:   "Backs up config.yaml, imports the original subscription and overrides, writes the config in place and reloads mihomo. If the coexistence checks get worse, the backup is written back.",
	TipSecure:  "Sets a secret on mihomo's controller so other programs can't switch nodes or change the config. Twinnel uses it on its own.",
	TipPlans:   "A plan only records what takeover would do; nothing is changed. Until you take over you can delete it and make a new one.",

	AboutTakeover:    "What is takeover",
	AbtTkYourConfig:  "your config.yaml",
	AbtTkSub:         "original subscription",
	AbtTkLayers:      "overrides",
	AbtTkComposed:    "config.yaml composed by Twinnel",
	AbtTkChanges:     "What takeover changes",
	AbtTkBackup:      "Your config.yaml is backed up as is first",
	AbtTkRewrite:     "config.yaml is rewritten in place, same path; mihomo reloads once, no restart",
	AbtTkKeep:        "mihomo's system service (plist) and binary are left alone",
	AbtTkAutoUndo:    "If the coexistence checks get worse, the backup is written back",
	AbtTkEdit:        "Changing the config after takeover",
	AbtTkEditBody:    "Don't edit config.yaml directly: the next apply overwrites it. Put your changes in an override.",
	AbtTkUndo:        "How to undo",
	AbtTkUndoBody:    "Stop Using Twinnel → Undo Takeover: the config.yaml from before is written back, the imported subscription and overrides are removed, the service stays.",
	AbtTkWhenNot:     "When not to take over",
	AbtTkWhenNotBody: "When you want to keep editing config.yaml by hand. Without takeover Twinnel still shows status, switches nodes and runs health checks.",
}
