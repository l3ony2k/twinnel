package i18n

// Texts of 设置这台 Mac (cmd/app/setuppage.go): the fresh-setup flow of
// core/setup. Embedded in Strings; fields are promoted (T.SetupTitle).

// SetupStrings holds them.
type SetupStrings struct {
	SetupTitle     string // the page title and the button that opens it
	SetupStepsHead string
	StepSetUpMac   string // 开始使用's step on a machine with nothing to take over
	SetupSteps     map[string]string
	SetupStart     string
	SetupContinue  string
	SetupRunning   string
	SetupSkipped   string
	SetupDone      string
	SetupOpenOverv string

	// The subscription card.
	SetupSubHead, SetupSubURL, SetupSubPlaceholder string
	SetupImport                                    string // opens a file picker
	SetupImported                                  string // %s: the zip's file name
	SetupImportSecret                              string

	// A failed download.
	SetupViaSub, SetupViaProxy, SetupProxyAddr, SetupProxyUse string

	// The login step.
	SetupOpenLogin string

	// Failure reasons by core/setup code.
	SetupWhyDownload, SetupWhyNotRoot, SetupWhyConfig, SetupWhyHealth string // SetupWhyHealth: %s the checks
	SetupNeedsSub                                                     string
	// SetupWhyInstall: installing failed after the download (setup
	// CodeInstallFailed); the coded message (MsgTexts) follows it.
	SetupWhyInstall string
	// SetupWhyDamaged: an installed program's files are damaged
	// (setup.CodeDamaged); the path stays behind 复制详情.
	SetupWhyDamaged string

	// Warnings by core/setup code.
	SetupWarnGeo, SetupWarnSystemVPN string
	SetupWarnTSOnPath                string // %s: the other tailscale command
	SetupWarnTSLink                  string
	SetupWarnRestart                 string // Windows: restart to finish installing Tailscale

	// ⓘ.
	TipSetup, TipSetupDNS, TipSetupProxy string
}

var zhSetup = SetupStrings{
	SetupTitle:     "设置这台 Mac",
	SetupStepsHead: "步骤",
	StepSetUpMac:   "设置这台 Mac",
	SetupSteps: map[string]string{
		"helper":       "安装后台助手",
		"subscription": "添加订阅",
		"mihomo":       "下载 mihomo",
		"config":       "合成配置",
		"apply":        "启动 mihomo",
		"dns":          "系统 DNS 指向 mihomo",
		"tailscale":    "安装 Tailscale",
		"login":        "登录 Tailscale",
		"health":       "健康检查",
	},
	SetupStart:     "开始设置",
	SetupContinue:  "继续",
	SetupRunning:   "正在设置…",
	SetupSkipped:   "跳过",
	SetupDone:      "这台 Mac 已设置好",
	SetupOpenOverv: "打开概览",

	SetupSubHead:        "订阅",
	SetupSubURL:         "订阅链接",
	SetupSubPlaceholder: "https://",
	SetupImport:         "从备份导入…",
	SetupImported:       "已读取 %s",
	SetupImportSecret:   "备份里有节点密码和订阅链接，设置完请删除",

	SetupViaSub:    "用订阅的节点下载",
	SetupViaProxy:  "用其他代理…",
	SetupProxyAddr: "代理地址",
	SetupProxyUse:  "用这个代理",

	SetupOpenLogin: "打开登录页",

	SetupWhyDownload: "下载失败",
	SetupWhyNotRoot:  "后台助手没有以管理员身份运行，系统改动只生成了计划",
	SetupWhyConfig:   "mihomo 不接受合成出的配置",
	SetupWhyHealth:   "未通过：%s",
	SetupNeedsSub:    "先填订阅链接，或从备份导入",
	SetupWhyInstall:  "安装没有完成",
	SetupWhyDamaged:  "Tailscale 的程序文件损坏了",

	SetupWarnGeo:       "GEO 数据库没有下载，mihomo 启动时会自己下载",
	SetupWarnSystemVPN: "保留了 Tailscale app，它占用系统 VPN 通道，会和 mihomo 的 TUN 冲突",
	SetupWarnTSOnPath:  "已经有一个 tailscale 命令（%s），没有替换它",
	SetupWarnTSLink:    "没能把 tailscale 命令放进命令行工具的文件夹",
	SetupWarnRestart:   "重启电脑后 Tailscale 才能装完",

	TipSetup:      "从零开始：安装后台助手和 mihomo，添加订阅，打开 TUN 和 Tailscale 共存，把系统 DNS 指向 mihomo，再装好并登录 Tailscale。已完成的步骤会自动跳过。",
	TipSetupDNS:   "把每个网络服务的 DNS 设为 127.0.0.1，由 mihomo 解析。原来的设置会保存，卸载后台助手时改回去。",
	TipSetupProxy: "GitHub 连不上时，用订阅里的节点临时代理这次下载；也可以填局域网里另一台机器的代理。",
}

var enSetup = SetupStrings{
	SetupTitle:     "Set Up This Mac",
	SetupStepsHead: "Steps",
	StepSetUpMac:   "Set up this Mac",
	SetupSteps: map[string]string{
		"helper":       "Install the helper",
		"subscription": "Add a subscription",
		"mihomo":       "Download mihomo",
		"config":       "Compose the config",
		"apply":        "Start mihomo",
		"dns":          "Point the system DNS at mihomo",
		"tailscale":    "Install Tailscale",
		"login":        "Log in to Tailscale",
		"health":       "Run the health checks",
	},
	SetupStart:     "Set Up",
	SetupContinue:  "Continue",
	SetupRunning:   "Setting up…",
	SetupSkipped:   "Skipped",
	SetupDone:      "This Mac is set up",
	SetupOpenOverv: "Open Overview",

	SetupSubHead:        "Subscription",
	SetupSubURL:         "Subscription URL",
	SetupSubPlaceholder: "https://",
	SetupImport:         "Import a Backup…",
	SetupImported:       "Read %s",
	SetupImportSecret:   "The backup holds node passwords and subscription URLs; delete it when you are done",

	SetupViaSub:    "Download Through the Subscription",
	SetupViaProxy:  "Use Another Proxy…",
	SetupProxyAddr: "Proxy address",
	SetupProxyUse:  "Use This Proxy",

	SetupOpenLogin: "Open Login Page",

	SetupWhyDownload: "The download failed",
	SetupWhyNotRoot:  "The helper is not running as administrator; the system changes were only planned",
	SetupWhyConfig:   "mihomo does not accept the composed config",
	SetupWhyHealth:   "Failed: %s",
	SetupNeedsSub:    "Enter a subscription URL or import a backup first",
	SetupWhyInstall:  "The installation didn't finish",
	SetupWhyDamaged:  "Tailscale's program files are damaged",

	SetupWarnGeo:       "The GEO databases did not download; mihomo downloads them when it starts",
	SetupWarnSystemVPN: "Kept the Tailscale app; it takes the system VPN slot, which conflicts with mihomo's TUN",
	SetupWarnTSOnPath:  "Another tailscale command is installed (%s); left as it is",
	SetupWarnTSLink:    "Couldn't put the tailscale command in the command line tool folder",
	SetupWarnRestart:   "Restart the computer to finish installing Tailscale",

	TipSetup:      "From nothing: installs the helper and mihomo, adds your subscription, turns on TUN and Tailscale coexistence, points the system DNS at mihomo, then installs and logs in to Tailscale. Steps already done are skipped.",
	TipSetupDNS:   "Sets the DNS of every network service to 127.0.0.1, so mihomo answers. The old settings are kept and put back when you uninstall the helper.",
	TipSetupProxy: "When GitHub can't be reached, the subscription's nodes carry this one download; or enter a proxy on another machine on your network.",
}
