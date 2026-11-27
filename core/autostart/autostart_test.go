package autostart

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	for _, tt := range []struct{ name, prog, want string }{
		{`\mihomo-party`, "", "Clash Party"},
		{`\mihomo-party-run`, `C:\Program Files\Clash Party\Clash Party.exe`, "Clash Party"},
		{"io.github.clash-verge-rev.clash-verge-rev", "", "Clash Verge"},
		{"com.example.mihomo", "/usr/local/bin/mihomo", "mihomo"},
		{"com.l3ony2k.twinnel.mihomo", "/Library/Application Support/twinnel/kernels/mihomo/current/mihomo", ""},
		{"OneDrive", `C:\Program Files\Microsoft OneDrive\OneDrive.exe`, ""},
	} {
		got, ok := Match(tt.name, tt.prog)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("Match(%q, %q) = %q %v, want %q", tt.name, tt.prog, got, ok, tt.want)
		}
	}
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTaskDir(t *testing.T) {
	dir := t.TempDir()
	b, err := os.ReadFile("testdata/task-utf16.xml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mihomo-party"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "mihomo-party-run"), `<?xml version="1.0" encoding="UTF-16"?><Task><Settings><Enabled>false</Enabled></Settings><Actions><Exec><Command>C:\Program Files\Clash Party\resources\sidecar\mihomo.exe</Command></Exec></Actions></Task>`)
	write(t, filepath.Join(dir, "Microsoft", "Windows", "Defrag", "ScheduledDefrag"), `<Task><Actions><Exec><Command>%windir%\system32\defrag.exe</Command></Exec></Actions></Task>`)
	got, err := Find(context.Background(), &TaskDirSource{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != `\mihomo-party` || !got[0].Enabled || got[0].Client != "Clash Party" ||
		got[0].Program != `C:\Program Files\Clash Party\Clash Party.exe` || got[1].Enabled || got[1].ID != `scheduled-task:\mihomo-party-run` {
		t.Fatalf("%+v", got)
	}
	cmds, err := Plan(got[0], OpDisable)
	if err != nil || len(cmds) != 1 || !strings.HasSuffix(cmds[0][0], `\System32\schtasks.exe`) || strings.Join(cmds[0][1:], " ") != `/Change /TN \mihomo-party /Disable` {
		t.Fatalf("%q %v", cmds, err)
	}
	cmds, _ = Plan(got[0], OpEnable)
	if cmds[0][len(cmds[0])-1] != "/Enable" {
		t.Fatalf("%q", cmds)
	}
}

func TestRunKeyPlan(t *testing.T) {
	e := Entry{Kind: RunKey, Name: "Clash Verge", Location: `HKU\S-1-5-21-1000-1000-1000-1001\Software\Microsoft\Windows\CurrentVersion\Run`, Disable: ByHelper}
	cmds, err := Plan(e, OpDisable)
	if err != nil {
		t.Fatal(err)
	}
	want := `add HKU\S-1-5-21-1000-1000-1000-1001\Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run /v Clash Verge /t REG_BINARY /d 030000000000000000000000 /f`
	if got := strings.Join(cmds[0][1:], " "); got != want {
		t.Fatalf("%s", got)
	}
	e.Location = `HKLM\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Run`
	cmds, _ = Plan(e, OpEnable)
	if cmds[0][2] != `HKLM\Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run32` || cmds[0][8] != "020000000000000000000000" {
		t.Fatalf("%q", cmds)
	}
}

func TestLaunchd(t *testing.T) {
	root := t.TempDir()
	agents, daemons, apps := filepath.Join(root, "LaunchAgents"), filepath.Join(root, "LaunchDaemons"), filepath.Join(root, "Applications")
	write(t, filepath.Join(agents, "io.github.clash-verge-rev.plist"), "<plist>clash-verge</plist>")
	write(t, filepath.Join(agents, "com.example.notes.plist"), "<plist>notes</plist>")
	write(t, filepath.Join(daemons, "com.example.mihomo.plist"), "<plist>mihomo</plist>")
	write(t, filepath.Join(daemons, "com.l3ony2k.twinnel.helper.plist"), "<plist>twinnel</plist>")
	write(t, filepath.Join(apps, "ClashX Meta.app", "Contents", "Library", "LoginItems", "Launcher.app", "x"), "")
	js := map[string]string{
		"io.github.clash-verge-rev.plist": `{"Label":"io.github.clash-verge-rev","ProgramArguments":["/Applications/Clash Verge.app/Contents/MacOS/clash-verge"]}`,
		"com.example.mihomo.plist":        `{"Label":"com.example.mihomo","Program":"/usr/local/bin/mihomo"}`,
	}
	src := &LaunchdSource{
		Dirs: []LaunchdDir{{Dir: agents, Kind: LaunchAgent, User: "501"}, {Dir: daemons, Kind: LaunchDaemon}},
		Apps: []string{apps},
		JSON: func(_ context.Context, p string) ([]byte, error) { return []byte(js[filepath.Base(p)]), nil },
		Disabled: func(_ context.Context, domain string) (map[string]bool, error) {
			if domain == "system" {
				return ParsePrintDisabled([]byte("disabled services = {\n\t\"com.example.mihomo\" => disabled\n}\n")), nil
			}
			return nil, nil
		},
	}
	got, err := Find(context.Background(), src, "com.example.other")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, e := range got {
		ids = append(ids, e.ID)
	}
	want := []string{"launch-agent:501:io.github.clash-verge-rev", "login-item:ClashX Meta", "launch-daemon:com.example.mihomo"}
	if !slices.Equal(ids, want) {
		t.Fatalf("%q\n%+v", ids, got)
	}
	if got[0].Disable != ByHelper || got[1].Disable != Guided || got[2].Enabled {
		t.Fatalf("%+v", got)
	}
	cmds, err := Plan(got[0], OpDisable)
	if err != nil || len(cmds) != 2 || strings.Join(cmds[0], " ") != "/bin/launchctl disable gui/501/io.github.clash-verge-rev" {
		t.Fatalf("%q %v", cmds, err)
	}
	if _, err := Plan(got[1], OpDisable); err == nil {
		t.Fatal("planned a guided entry")
	}
	// The service Twinnel adopted is not a competitor.
	got, _ = Find(context.Background(), src, "com.example.mihomo")
	for _, e := range got {
		if e.Name == "com.example.mihomo" {
			t.Fatal("listed the adopted service")
		}
	}
}

func TestLinux(t *testing.T) {
	root := t.TempDir()
	sys := filepath.Join(root, "etc", "systemd", "system")
	write(t, filepath.Join(sys, "clash-verge-service.service"), "[Service]\nExecStart=/usr/bin/clash-verge-service\n")
	if err := os.MkdirAll(filepath.Join(sys, "multi-user.target.wants"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.Symlink("../clash-verge-service.service", filepath.Join(sys, "multi-user.target.wants", "clash-verge-service.service"))
	home := filepath.Join(root, "home", "alice")
	write(t, filepath.Join(home, ".config", "systemd", "user", "mihomo.service"), "[Service]\nExecStart=-/home/alice/bin/mihomo -d /home/alice/.config/mihomo\n")
	write(t, filepath.Join(home, ".config", "autostart", "flclash.desktop"), "[Desktop Entry]\nExec=/opt/FlClash/FlClash %U\n")
	write(t, filepath.Join(root, "xdg", "clash-nyanpasu.desktop"), "[Desktop Entry]\nExec=clash-nyanpasu\nHidden=true\n")
	src := &LinuxSource{SystemDirs: []string{sys}, WantsRoot: sys, XDGDirs: []string{filepath.Join(root, "xdg")}, Homes: []LinuxHome{{Home: home, User: "alice"}}}
	got, err := Find(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	by := map[Kind]Entry{}
	for _, e := range got {
		by[e.Kind] = e
	}
	if e := by[SystemdSystem]; !e.Enabled || e.Disable != ByHelper || e.Client != "Clash Verge" {
		t.Errorf("system unit %+v", e)
	}
	if e := by[SystemdUser]; e.Enabled || e.Program != "/home/alice/bin/mihomo" || !strings.Contains(e.Command, "systemctl --user disable --now mihomo.service") {
		t.Errorf("user unit %+v", e)
	}
	if len(got) != 4 {
		t.Fatalf("%+v", got)
	}
	cmds, _ := Plan(by[SystemdSystem], OpDisable)
	if strings.Join(cmds[0], " ") != "systemctl disable --now clash-verge-service.service" {
		t.Fatalf("%q", cmds)
	}
}
