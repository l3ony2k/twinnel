package main

import (
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/service"
)

func TestParsePlan(t *testing.T) {
	var b strings.Builder
	b.WriteString("# duet helper install --dry-run: nothing below has been run.\n")
	service.FormatPlan(&b, []service.Step{
		{Kind: service.StepMkdir, Path: "/Library/Application Support/duet", Mode: 0o755},
		{Kind: service.StepCopy, Src: "/x/duet-helper", Path: "/Library/PrivilegedHelperTools/h", Mode: 0o755},
		{Kind: service.StepRun, Argv: []string{"/bin/launchctl", "bootout", "system/h"}, IgnoreError: true},
		{Kind: service.StepWrite, Path: "/Library/LaunchDaemons/h.plist", Mode: 0o644, Content: []byte("<plist>\n</plist>\n")},
		{Kind: service.StepRun, Argv: []string{"/bin/launchctl", "bootstrap", "system", "/Library/LaunchDaemons/h.plist"}},
		{Kind: service.StepRun, Argv: []string{"/usr/sbin/groupadd", "--system", "duet"}},
	})
	steps, ok := parsePlan(b.String())
	if !ok || len(steps) != 6 {
		t.Fatalf("steps = %+v", steps)
	}
	want := []string{
		"创建目录 /Library/Application Support/duet",
		"把后台助手复制到 /Library/PrivilegedHelperTools/h",
		"停掉旧的后台助手（如果有）",
		"写入服务定义 /Library/LaunchDaemons/h.plist",
		"加载服务：后台助手以 root 启动",
		"/usr/sbin/groupadd --system duet", // not known: the command itself
	}
	for i, w := range want {
		if steps[i].what != w {
			t.Errorf("step %d: %q, want %q", i+1, steps[i].what, w)
		}
	}
	if steps[3].content != "<plist>\n</plist>" {
		t.Errorf("content %q", steps[3].content)
	}
	if _, ok := parsePlan("error: something"); ok {
		t.Error("parsed garbage")
	}
}
