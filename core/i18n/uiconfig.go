package i18n

// Texts added by the ui-config pass (logs, layers, history, health,
// sidebar state). They live in their own struct so parallel work on
// Strings does not collide; fold them into Strings when convenient.

// MoreStrings holds the texts of the ui-config pass.
type MoreStrings struct {
	// Log levels, in the order of the level control (debug, info,
	// warning, error); the API keeps its English names.
	LogLevelNames []string
	LogPaused     string // tooltip of the pause toggle while paused

	// Layers.
	LayerRemoveBody   string
	LayerOpen         string // context menu: open in the editor
	LayerEnable       string
	LayerDisable      string
	LayerReorderHint  string // tooltip of a stored layer row
	LayerManagedFixed string // tooltip of a managed row: why it does not move
	CopyID            string
	CopyName          string
	CopyLine          string
	CopyText          string

	// History.
	HistoryEmptyHint string

	// Health.
	HealthCopySummary string

	// Sidebar state, as a tooltip and spoken label: "mihomo：运行中".
	StateLabel string // %s: name, %s: state
}

// M is the current language's ui-config texts.
var M = &zhMore

var zhMore = MoreStrings{
	LogLevelNames: []string{"调试", "信息", "警告", "错误"},
	LogPaused:     "已暂停：新日志不会显示",

	LayerRemoveBody:   "覆写内容会从后台助手里删除，不能恢复。正在使用的配置要等下次应用后才会去掉它。",
	LayerOpen:         "打开",
	LayerEnable:       "启用",
	LayerDisable:      "停用",
	LayerReorderHint:  "拖动可以调整顺序（⌥⌘↑ / ⌥⌘↓）",
	LayerManagedFixed: "托管层由 duet 生成，固定在用户覆写之后",
	CopyID:            "复制 ID",
	CopyName:          "复制名称",
	CopyLine:          "复制这一行",
	CopyText:          "复制",

	HistoryEmptyHint: "每次应用配置都会在这里留一条记录，可以对比或回滚到任一条。",

	HealthCopySummary: "复制这项检查",

	StateLabel: "%s：%s",
}
