package main

import (
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/platform/windows/scm"
)

func TestParsePlan(t *testing.T) {
	var b strings.Builder
	b.WriteString("# twinnel helper install --dry-run: nothing below has been run.\n")
	service.FormatPlan(&b, []service.Step{
		{Kind: service.StepMkdir, Path: "/Library/Application Support/twinnel", Mode: 0o755},
		{Kind: service.StepCopy, Src: "/x/twinnel-helper", Path: "/Library/PrivilegedHelperTools/h", Mode: 0o755},
		{Kind: service.StepRun, Argv: []string{"/bin/launchctl", "bootout", "system/h"}, IgnoreError: true},
		{Kind: service.StepWrite, Path: "/Library/LaunchDaemons/h.plist", Mode: 0o644, Content: []byte("<plist>\n</plist>\n")},
		{Kind: service.StepRun, Argv: []string{"/bin/launchctl", "bootstrap", "system", "/Library/LaunchDaemons/h.plist"}},
		{Kind: service.StepRun, Argv: []string{"/usr/sbin/groupadd", "--system", "twinnel"}},
		{Kind: service.StepWait, Argv: []string{"/bin/launchctl", "print", "system/h"}, Until: "Could not find service", Timeout: 10e9, IgnoreError: true},
		{Kind: service.StepRun, Argv: []string{"/bin/launchctl", "kickstart", "-k", "system/h"}},
	})
	steps, ok := parsePlan(b.String())
	if !ok || len(steps) != 8 {
		t.Fatalf("steps = %+v", steps)
	}
	want := []string{
		"创建目录 /Library/Application Support/twinnel",
		"把后台助手复制到 /Library/PrivilegedHelperTools/h",
		"停掉旧的后台助手（如果有）",
		"写入服务定义 /Library/LaunchDaemons/h.plist",
		"加载服务：后台助手以 root 启动",
		"/usr/sbin/groupadd --system twinnel", // not known: the command itself
		"等旧的后台助手完全停下",
		"重新启动后台助手",
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

func TestTileLayout(t *testing.T) {
	for _, tc := range []struct {
		w       float32
		n, cols int
		capped  bool
	}{
		{850, 12, 3, false}, {600, 12, 2, false}, {1100, 12, 4, false}, {300, 12, 1, false},
		{850, 2, 2, true}, {1100, 1, 1, true}, {0, 12, 3, false},
	} {
		cols, maxW := tileLayout(tc.w, tc.n)
		if cols != tc.cols || (maxW > 0) != tc.capped {
			t.Errorf("tileLayout(%v, %d) = %d, %v", tc.w, tc.n, cols, maxW)
			continue
		}
		if tc.w > 0 && !tc.capped && tc.cols > 1 {
			tile := (tc.w - float32(cols-1)*tileGap) / float32(cols)
			if tile < tileMin || tile > tileMax {
				t.Errorf("w %v: tile %v outside %d–%d", tc.w, tile, tileMin, tileMax)
			}
		}
	}
}

// The Windows plan (platform/windows/scm): every step is worded.
func TestParsePlanWindows(t *testing.T) {
	i18n.Set("zh-Hans")
	plan, err := scm.HelperInstallPlan(`C:\Users\x\AppData\Local\Programs\Twinnel\bin\twinnel-helper.exe`, `C:\ProgramData\Twinnel`, `\\.\pipe\twinnel-helper`, "TwinnelHelper", []string{"S-1-5-21-1-2-3-1001"})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	service.FormatPlan(&b, plan)
	steps, ok := parsePlan(b.String())
	if !ok || len(steps) != len(plan) {
		t.Fatalf("steps = %+v", steps)
	}
	for i, st := range steps {
		if st.what == st.cmd {
			t.Errorf("step %d not worded: %s", i+1, st.cmd)
		}
	}
	if !strings.Contains(b.String(), "sc.exe") {
		t.Fatalf("plan without sc.exe steps:\n%s", b.String())
	}
}
