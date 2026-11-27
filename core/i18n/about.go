package i18n

// Texts of the 关于 page: version and update check, component versions,
// the getting-started steps, the FAQ and the links. Embedded in Strings;
// fields are promoted (T.PageAbout).

// AboutStrings holds them.
type AboutStrings struct {
	PageAbout    string
	AboutTagline string

	// 检查更新 and its outcomes.
	CheckUpdate       string
	UpdateChecking    string
	UpdateLatest      string
	UpdateNewVersion  string // %s: the new version
	UpdateDownload    string
	UpdateNone        string // no release published (yet)
	UpdateFailed      string // %s: the short reason
	UpdateFailedPlain string // no reason duet can word
	UpdateRateLimit   string

	// 基本信息.
	AboutBasics, AboutPlatform string

	// 组件版本.
	Components, CompApp, CompCLI, CompHelper string

	// 入门.
	GettingStarted                                        string
	StepHelper, StepSetup, StepHealth, StepDaily          string
	StepInstall, HelperInstalledTitle, NavBack            string
	FAQ                                                   string
	FAQVPNQ, FAQVPNA, FAQReadOnlyQ, FAQReadOnlyA          string
	FAQRollbackQ, FAQRollbackA, FAQQuitQ, FAQQuitA        string
	Links, LinkRepo, LinkLicense, LinkNotices, LinkIssues string
}

var zhAbout = AboutStrings{
	PageAbout:    "关于",
	AboutTagline: "让 mihomo 与 Tailscale 在同一台电脑上和平共处",

	CheckUpdate:       "检查更新",
	UpdateChecking:    "检查中…",
	UpdateLatest:      "已是最新",
	UpdateNewVersion:  "有新版本 %s",
	UpdateDownload:    "下载",
	UpdateNone:        "暂无发布版本",
	UpdateFailed:      "检查失败：%s",
	UpdateFailedPlain: "检查失败",
	UpdateRateLimit:   "GitHub 请求太频繁，稍后再试",

	AboutBasics:   "基本信息",
	AboutPlatform: "平台",

	Components: "组件版本",
	CompApp:    "duet App",
	CompCLI:    "命令行工具",
	CompHelper: "后台助手",

	GettingStarted:       "入门",
	StepHelper:           "安装后台助手",
	StepSetup:            "接管现有配置，或添加订阅",
	StepHealth:           "检查健康状态",
	StepDaily:            "日常使用：切换节点、查看 Tailscale 设备",
	StepInstall:          "安装",
	HelperInstalledTitle: "后台助手已安装",
	NavBack:              "返回",

	FAQ:          "常见问题",
	FAQVPNQ:      "为什么 duet 不占用系统 VPN 通道？",
	FAQVPNA:      "mihomo 和 tailscaled 各用自己的虚拟网卡，系统 VPN 通道留空；duet 只生成让两者共存的设置。",
	FAQReadOnlyQ: "为什么有些按钮带锁？",
	FAQReadOnlyA: "带锁表示现在还做不了：切换节点和模式要能连上 mihomo，改配置、服务和内核要先安装后台助手。把指针放在锁上可以看原因。",
	FAQRollbackQ: "配置改坏了怎么回滚？",
	FAQRollbackA: "在「历史」里选一个版本，点「回滚到这里」。内核在「内核」页回滚。",
	FAQQuitQ:     "退出 duet 会断网吗？",
	FAQQuitA:     "不会。mihomo 和 Tailscale 作为系统服务运行，duet 只是控制面板。",

	Links:       "链接",
	LinkRepo:    "GitHub 仓库",
	LinkLicense: "许可证（MIT）",
	LinkNotices: "第三方声明",
	LinkIssues:  "报告问题",
}
