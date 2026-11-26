package i18n

// Texts of the config-side pages (logs, layers, history, health, sidebar
// state). Their own struct, embedded in Strings, so parallel work on
// i18n.go does not collide; fields are promoted (T.LogLevelNames).

// ConfigStrings holds the texts of the config-side pages.
type ConfigStrings struct {
	// Log levels, in the order of the level control (debug, info,
	// warning, error); the API keeps its English names.
	LogLevelNames []string
	LogPaused     string // tooltip of the pause toggle while paused

	// Layers.
	LayerRemoveDetail string
	LayerOpen         string // context menu: open in the editor
	LayerEnable       string
	LayerDisable      string
	LayerReorderHint  string // tooltip of a stored layer row
	LayerManagedFixed string // tooltip of a managed row: why it does not move
	CopyID            string
	CopyName          string
	CopyLine          string
	CopyText          string

	// Layers, empty: a row-size line under the heading's 新建覆写.
	LayersNone string

	// History.
	HistoryEmptyHint string

	// Health.
	HealthCopySummary string
	HealthFailedTitle string
	HealthFailedHint  string

	// Sidebar state, as a tooltip and spoken label: "mihomo：运行中".
	StateLabel string // %s: name, %s: state
}

var zhConfig = ConfigStrings{
	LogLevelNames: []string{"调试", "信息", "警告", "错误"},
	LogPaused:     "已暂停：新日志不会显示",

	LayerRemoveDetail: "覆写内容会从后台助手里删除，不能恢复。正在使用的配置要等下次应用后才会去掉它。",
	LayerOpen:         "打开",
	LayerEnable:       "启用",
	LayerDisable:      "停用",
	LayerReorderHint:  "拖动可以调整顺序（⌥⌘↑ / ⌥⌘↓）",
	LayerManagedFixed: "托管层由 duet 生成，固定在用户覆写之后",
	CopyID:            "复制 ID",
	CopyName:          "复制名称",
	CopyLine:          "复制这一行",
	CopyText:          "复制",

	LayersNone: "还没有自己的覆写，用「新建覆写」写第一层",

	HistoryEmptyHint: "每次应用配置都会在这里留一条记录，可以对比或回滚到任一条。",

	HealthCopySummary: "复制这项检查",
	HealthFailedTitle: "没能完成健康检查",
	HealthFailedHint:  "检查本身出了错，网络设置没有被改动。可以重新检查一次。",

	StateLabel: "%s：%s",
}
