package scm

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/service"
)

// unixWords are Unix commands and words a Windows plan must not print.
var unixWords = regexp.MustCompile(`\b(rm|install|mv|chmod|chown|launchctl|systemctl|sudo)\b|-m 0\d{3}|\broot\b|Twinnel\.app|(^|\s)'`)

func TestWindowsDialect(t *testing.T) {
	steps := []service.Step{
		{Kind: service.StepMkdir, Path: `C:\Program Files\Twinnel`, Mode: 0o755},
		{Kind: service.StepCopy, Src: `C:\Users\x\bin\twinnel-helper.exe`, Path: `C:\Program Files\Twinnel\twinnel-helper.exe`, Mode: 0o755},
		{Kind: service.StepWrite, Path: `C:\ProgramData\Twinnel\x.json`, Content: []byte("{}")},
		{Kind: service.StepRemove, Path: `C:\Program Files\Twinnel\twinnel-helper.exe`, IgnoreError: true},
		{Kind: service.StepRemoveAll, Path: `C:\ProgramData\Twinnel`},
		{Kind: service.StepRename, Src: `C:\Program Files\Twinnel\twinnel-svchost.exe`, Path: `C:\Program Files\Twinnel\twinnel-svchost.old-1.exe`},
		{Kind: service.StepRun, Argv: []string{SC, "description", "TwinnelHelper", "Twinnel's helper"}},
		{Kind: service.StepWait, Argv: []string{SC, "query", "TwinnelHelper"}, Until: StateStopped, Timeout: 30 * time.Second},
	}
	var b strings.Builder
	Dialect.FormatPlan(&b, steps)
	want := `1. mkdir "C:\Program Files\Twinnel"
2. copy /y C:\Users\x\bin\twinnel-helper.exe "C:\Program Files\Twinnel\twinnel-helper.exe"
3. write C:\ProgramData\Twinnel\x.json (2 bytes)
----- C:\ProgramData\Twinnel\x.json -----
{}
----- end -----
4. del /f "C:\Program Files\Twinnel\twinnel-helper.exe"  (errors ignored)
5. rmdir /s /q C:\ProgramData\Twinnel
6. move /y "C:\Program Files\Twinnel\twinnel-svchost.exe" "C:\Program Files\Twinnel\twinnel-svchost.old-1.exe"
7. sc.exe description TwinnelHelper "Twinnel's helper"
8. wait until ` + "`sc.exe query TwinnelHelper`" + ` says "STOPPED" (up to 30s)
`
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
	if Dialect.AsAdmin() != "as administrator" || Dialect.App != "Twinnel" {
		t.Errorf("words: %q %q", Dialect.AsAdmin(), Dialect.App)
	}
}

// Every plan the Windows code makes reads in Windows terms.
func TestWindowsPlansHaveNoUnixWords(t *testing.T) {
	m := &Manager{HostSrc: `C:\Program Files\Twinnel\twinnel-helper.exe`}
	var plans [][]service.Step
	add := func(s []service.Step, err error) {
		if err != nil {
			t.Fatal(err)
		}
		plans = append(plans, s)
	}
	add(HelperInstallPlan(`C:\Users\x\AppData\Local\Programs\Twinnel\bin\twinnel-helper.exe`, `C:\ProgramData\Twinnel`, `\\.\pipe\twinnel-helper`, "TwinnelHelper", []string{"S-1-5-21-1-2-3-1001"}))
	add(HelperUninstallPlan("TwinnelHelper"))
	spec := service.MihomoSpec("TwinnelMihomo", `C:\ProgramData\Twinnel\kernels\mihomo\current\mihomo.exe`, `C:\ProgramData\Twinnel\mihomo`, `C:\ProgramData\Twinnel\mihomo\config.yaml`, `C:\ProgramData\Twinnel\logs`)
	for _, op := range []service.Op{service.OpInstall, service.OpUninstall, service.OpStart, service.OpStop, service.OpRestart, service.OpDisable, service.OpEnable} {
		add(m.Plan(op, spec))
	}
	host := Host{Path: `C:\Program Files\Twinnel\twinnel-svchost.exe`, Present: true, Version: "0.1.0",
		Services: []HostedService{{Label: "TwinnelMihomo", Running: true}}, Aside: []string{`C:\Program Files\Twinnel\twinnel-svchost.old-1.exe`}}
	add(host.RefreshPlan(`C:\Program Files\Twinnel\twinnel-helper.exe`, 1, time.Unix(2, 0)), nil)
	for _, p := range plans {
		var b strings.Builder
		Dialect.FormatPlan(&b, p)
		for _, l := range strings.Split(b.String(), "\n") {
			if unixWords.MatchString(l) {
				t.Errorf("Unix words: %s", l)
			}
		}
	}
}
