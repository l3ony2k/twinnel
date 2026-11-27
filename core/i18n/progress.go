package i18n

// ProgressStrings say what a long operation is doing while it runs
// (save-perf): an apply's stages, as the helper reports them
// (compose.stage), and a preview's steps. Shown in place of the apply
// card's state once the wait is long enough to show a spinner.
type ProgressStrings struct {
	StageValidate string // compose and mihomo -t
	StageWrite    string // back up and write the config file
	StageReload   string // mihomo loads it
	StageVerify   string // waiting for mihomo to answer again
	StageDiff     string // a preview loads the two texts for its diff
	Saving        string // an override's or subscription's save button while it runs
}

var zhProgress = ProgressStrings{
	StageValidate: "正在校验配置…",
	StageWrite:    "正在写入配置…",
	StageReload:   "正在让 mihomo 载入新配置…",
	StageVerify:   "正在等待 mihomo 响应…",
	StageDiff:     "正在与运行中的配置比较…",
	Saving:        "正在保存…",
}

var enProgress = ProgressStrings{
	StageValidate: "Checking the config…",
	StageWrite:    "Writing the config…",
	StageReload:   "Loading the new config into mihomo…",
	StageVerify:   "Waiting for mihomo to respond…",
	StageDiff:     "Comparing with the running config…",
	Saving:        "Saving…",
}
