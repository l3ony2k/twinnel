package i18n

// Texts of the main window's own parts added late (node tiles, peer rows,
// the 显示 menu). Their own struct, embedded in Strings as the other
// per-area sets are; fields are promoted (T.NodeUse).

// WindowStrings holds them.
type WindowStrings struct {
	// Context menus of a policy group's head and its node tiles.
	GroupExpand   string
	GroupCollapse string
	NodeUse       string // pick this node for the group
	CopyNodeName  string

	// The app menu's 显示 (View) menu, which lists the pages with ⌘1…⌘9.
	ViewMenu string

	// A diff's folded unchanged lines (cmd/app diffSeparator).
	DiffExpandUp, DiffExpandDown, DiffExpandAll string
	DiffUnchanged                               string // %d: lines

	// 设置 → 语言: the card, its row, and the choice that follows the
	// system (the languages are named in themselves, Lang.Name).
	Language, LanguageLabel, LanguageSystem string

	// LabelSep joins a label and what it labels in running text
	// ("失败：tailnet 路由").
	LabelSep string
	// CountF is a heading with a count ("有意的修改（4）"; %s, %d);
	// AskF makes a title a confirmation question (%s).
	CountF, AskF string
}

var zhWindow = WindowStrings{
	GroupExpand:   "展开",
	GroupCollapse: "收起",
	NodeUse:       "使用这个节点",
	CopyNodeName:  "复制节点名",
	ViewMenu:      "显示",

	DiffExpandUp:   "向上展开",
	DiffExpandDown: "向下展开",
	DiffExpandAll:  "全部展开",
	DiffUnchanged:  "%d 行未改动",

	Language:       "语言",
	LanguageLabel:  "界面语言",
	LanguageSystem: "跟随系统",

	LabelSep: "：",
	CountF:   "%s（%d）",
	AskF:     "%s？",
}
