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

	// Health.
	HealthCopySummary string
	HealthFailedTitle string

	// Sidebar state, as a tooltip and spoken label: "mihomo：运行中".
	StateLabel string // %s: name, %s: state
}

var zhConfig = ConfigStrings{
	LogLevelNames: []string{"调试", "信息", "警告", "错误"},
	LogPaused:     "已暂停：新日志不会显示",

	LayerRemoveDetail: "删除后不能恢复；下次应用时从配置里去掉。",
	LayerOpen:         "打开",
	LayerEnable:       "启用",
	LayerDisable:      "停用",
	LayerReorderHint:  "拖动可以调整顺序（⌥⌘↑ / ⌥⌘↓）",
	LayerManagedFixed: "托管覆写由 duet 生成，固定在用户覆写之后",
	CopyID:            "复制 ID",
	CopyName:          "复制名称",
	CopyLine:          "复制这一行",
	CopyText:          "复制",

	LayersNone: "还没有覆写",

	HealthCopySummary: "复制这项检查",
	HealthFailedTitle: "没能完成健康检查",

	StateLabel: "%s：%s",
}
