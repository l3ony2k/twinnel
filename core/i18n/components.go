package i18n

// ComponentStrings are the texts of 内核 → 组件: the GEO databases and the
// web dashboard (cmd/app/components.go). Each section always says what is
// installed and offers the one action that fits.
type ComponentStrings struct {
	// One line under a component that is not installed: what it is for.
	CompGeoWhat, CompUIWhat string

	CompDownload     string // 下载
	CompDownloadSize string // %s: size
	CompDownloadMiss string // some geo files are missing
	CompUpdate       string // 更新
	CompUpdateSize   string // %s: size
	CompCheck        string // 检查更新 (one section)
	CompRetry        string

	CompInstalled    string // label: 已安装
	CompVersion      string // label: 版本
	CompDate         string // label: 日期 (data without a version, e.g. GEO)
	CompSource       string // label: 来源
	CompSourceOther  string // installed by mihomo itself or by hand
	CompUpToDate     string // %s: HH:MM or a date
	CompNotChecked   string
	CompNewer        string // %s → %s
	CompNewerSize    string // %s → %s, %s size
	CompMissingFiles string // %s: names

	// Progress of a download (accessory.progress).
	CompStageDownload    string // %d percent, %s: file
	CompStageDownloadAny string // %s: file, size unknown
	CompStageVerify      string
	CompStageInstall     string
	CompStageReload      string
	CompStageConfigure   string // the dashboard: external-ui added, applying

	// Results.
	CompDone        string // %s: names
	CompDoneReload  string // mihomo uses the new data
	CompDoneNext    string // mihomo reads it at its next start
	CompDoneUI      string // %s: version
	CompNothing     string
	CompReloadLater string // the reload did not happen: apply to use
	CompFailed      string // %s: the component's title
	CompViaSub      string // retry through the subscription's nodes
	CompViaProxy    string
	CompViaHint     string // ⓘ body for the download routes

	// The dashboard.
	CompOpenUI       string
	CompEnableUI     string // files there, but the config serves none
	CompUINotServed  string // the config has no external-ui
	CompSecretCopied string // the secret went to the clipboard, not the URL
	CompUIStopped    string // mihomo does not answer: the dashboard can't open

	// Weekly auto-update of the GEO databases.
	CompAutoWeekly string
	TipGeoAuto     string
	CompAutoFailed string // %s: when
	CompAutoLast   string // %s: when

	TipUI string // ⓘ for the dashboard
}

var zhComponents = ComponentStrings{
	CompGeoWhat: "GEOIP、GEOSITE 这类规则要用到它；没有它，这些规则不生效。",
	CompUIWhat:  "在浏览器里看连接、切节点的网页面板。Twinnel 本身不需要它。",

	CompDownload:     "下载",
	CompDownloadSize: "下载 · %s",
	CompDownloadMiss: "下载缺少的文件",
	CompUpdate:       "更新",
	CompUpdateSize:   "更新 · %s",
	CompCheck:        "检查更新",
	CompRetry:        "重试",

	CompInstalled:    "已安装",
	CompVersion:      "版本",
	CompDate:         "日期",
	CompSource:       "来源",
	CompSourceOther:  "不是 Twinnel 下载的",
	CompUpToDate:     "已是最新 · 检查于 %s",
	CompNotChecked:   "还没检查过更新",
	CompNewer:        "有新版本：%s → %s",
	CompNewerSize:    "有新版本：%s → %s · %s",
	CompMissingFiles: "缺少：%s",

	CompStageDownload:    "下载中 %d%% · %s",
	CompStageDownloadAny: "下载中 · %s",
	CompStageVerify:      "正在校验…",
	CompStageInstall:     "正在安装…",
	CompStageReload:      "正在让 mihomo 重新加载…",
	CompStageConfigure:   "正在启用控制面板…",

	CompDone:        "已更新：%s",
	CompDoneReload:  "mihomo 已在用新数据",
	CompDoneNext:    "mihomo 下次启动时读取",
	CompDoneUI:      "控制面板已是 %s",
	CompNothing:     "都已是最新",
	CompReloadLater: "新数据还没生效",
	CompFailed:      "没能下载 %s",
	CompViaSub:      "用订阅的节点下载",
	CompViaProxy:    "用其他代理…",
	CompViaHint:     "GitHub 连不上时，用订阅里的节点临时代理这次下载；也可以填局域网里另一台机器的代理。",

	CompOpenUI:       "打开控制面板",
	CompEnableUI:     "启用控制面板",
	CompUINotServed:  "文件已在，但当前配置没有启用它",
	CompSecretCopied: "控制接口密钥已复制，在面板里粘贴即可登录",
	CompUIStopped:    "mihomo 没在运行，面板打不开",

	CompAutoWeekly: "每周自动更新",
	TipGeoAuto:     "开着时，后台助手每周下载一次新的 GEO 数据库，校验后让 mihomo 重新加载。默认关闭。",
	CompAutoFailed: "上次自动更新失败（%s）",
	CompAutoLast:   "上次自动更新：%s",

	TipUI: "mihomo 自带的网页面板，通过控制接口在浏览器里打开。只是另一种查看方式，Twinnel 的功能不依赖它。",
}

var enComponents = ComponentStrings{
	CompGeoWhat: "Rules like GEOIP and GEOSITE need these; without them those rules don't work.",
	CompUIWhat:  "A web page for watching connections and switching nodes in the browser. Twinnel itself doesn't need it.",

	CompDownload:     "Download",
	CompDownloadSize: "Download · %s",
	CompDownloadMiss: "Download Missing Files",
	CompUpdate:       "Update",
	CompUpdateSize:   "Update · %s",
	CompCheck:        "Check for Updates",
	CompRetry:        "Try Again",

	CompInstalled:    "Installed",
	CompVersion:      "Version",
	CompDate:         "Date",
	CompSource:       "Source",
	CompSourceOther:  "Not downloaded by Twinnel",
	CompUpToDate:     "Up to date · checked %s",
	CompNotChecked:   "Not checked for updates yet",
	CompNewer:        "Newer version: %s → %s",
	CompNewerSize:    "Newer version: %s → %s · %s",
	CompMissingFiles: "Missing: %s",

	CompStageDownload:    "Downloading %d%% · %s",
	CompStageDownloadAny: "Downloading · %s",
	CompStageVerify:      "Verifying…",
	CompStageInstall:     "Installing…",
	CompStageReload:      "Having mihomo reload…",
	CompStageConfigure:   "Turning on the dashboard…",

	CompDone:        "Updated: %s",
	CompDoneReload:  "mihomo is using the new data",
	CompDoneNext:    "mihomo reads it at its next start",
	CompDoneUI:      "The dashboard is now %s",
	CompNothing:     "Everything is up to date",
	CompReloadLater: "The new data isn't in use yet",
	CompFailed:      "Couldn't download %s",
	CompViaSub:      "Download Through Subscription Nodes",
	CompViaProxy:    "Use Another Proxy…",
	CompViaHint:     "When GitHub can't be reached, a node from your subscription can proxy this one download; or enter a proxy on another machine on your network.",

	CompOpenUI:       "Open Dashboard",
	CompEnableUI:     "Turn On Dashboard",
	CompUINotServed:  "The files are there, but the current config doesn't serve them",
	CompSecretCopied: "Controller secret copied; paste it in the dashboard to sign in",
	CompUIStopped:    "mihomo isn't running, so the dashboard can't open",

	CompAutoWeekly: "Update weekly",
	TipGeoAuto:     "When on, the helper downloads new GEO databases once a week, verifies them and has mihomo reload. Off by default.",
	CompAutoFailed: "Last automatic update failed (%s)",
	CompAutoLast:   "Last automatic update: %s",

	TipUI: "mihomo's web dashboard, opened in the browser through the controller. Just another view: nothing in Twinnel depends on it.",
}
