package foreign

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// All paths and names below are synthetic.

func TestIdentify(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want Client
	}{
		{[]string{`C:\Program Files\Clash Party\resources\sidecar\mihomo.exe`}, ClientClashParty},
		{[]string{"/Applications/Clash Party.app/Contents/Resources/sidecar/mihomo-smart"}, ClientClashParty},
		{[]string{"/usr/bin/mihomo", "", "/home/someone/.config/mihomo-party/work"}, ClientClashParty},
		{[]string{`C:\Users\someone\AppData\Roaming\mihomo-party\work`}, ClientClashParty},
		{[]string{"/Library/PrivilegedHelperTools/party.mihomo.helper"}, ClientClashParty},
		{[]string{`C:\Program Files\Clash Verge\verge-mihomo.exe`}, ClientVerge},
		{[]string{"/Users/someone/Library/Application Support/io.github.clash-verge-rev.clash-verge-rev"}, ClientVerge},
		{[]string{"/Applications/ClashX Meta.app/Contents/MacOS/ClashX Meta"}, ClientClashXMeta},
		{[]string{"/Library/PrivilegedHelperTools/com.metacubex.ClashX.ProxyConfigHelper.meta"}, ClientClashXMeta},
		{[]string{`C:\Program Files\FlClash\FlClashCore.exe`}, ClientFlClash},
		{[]string{"/home/someone/.local/share/com.follow.clash"}, ClientFlClash},
		{[]string{"/usr/local/bin/mihomo", "/home/someone/mihomo"}, ""},
	} {
		if got := Identify(tc.in...); got != tc.want {
			t.Errorf("Identify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, c := range Clients {
		if c.Name() == "" {
			t.Errorf("%s has no name", c)
		}
	}
}

func TestWorkDir(t *testing.T) {
	if d := WorkDir(Process{Args: []string{"mihomo", "-d", "/x/work", "-f", "/x/work/config.yaml"}, Cwd: "/"}); d != "/x/work" {
		t.Error(d)
	}
	if d := WorkDir(Process{Args: []string{"mihomo", "-d=/y"}}); d != "/y" {
		t.Error(d)
	}
	if d := WorkDir(Process{Cwd: "/z"}); d != "/z" {
		t.Error(d)
	}
}

func TestClassify(t *testing.T) {
	now := time.Unix(1000, 0)
	kdir := t.TempDir()
	exp := Expect{Binaries: []string{"/opt/adopted/mihomo"}, Dirs: []string{kdir}, PIDs: []int{77}}
	for _, tc := range []struct {
		name string
		p    *Process
		want Verdict
		cl   Client
	}{
		{"nothing listens", nil, Unknown, ""},
		{"service pid", &Process{PID: 77, Exe: "/somewhere/else"}, Managed, ""},
		{"child of the service wrapper", &Process{PID: 78, PPID: 77, Exe: `C:\ProgramData\Twinnel\kernels\mihomo\v1\mihomo.exe`}, Managed, ""},
		{"adopted binary", &Process{PID: 5, Exe: "/opt/adopted/mihomo"}, Managed, ""},
		{"managed kernel dir", &Process{PID: 5, Exe: filepath.Join(kdir, "current", "mihomo")}, Managed, ""},
		{"clash party", &Process{PID: 9, Exe: `C:\Program Files\Clash Party\resources\sidecar\mihomo.exe`, ParentExe: `C:\Program Files\Clash Party\Clash Party.exe`}, Foreign, ClientClashParty},
		{"unknown mihomo", &Process{PID: 9, Exe: "/usr/local/bin/mihomo"}, Foreign, ""},
		{"client known by its parent, own path unreadable", &Process{PID: 9, ParentExe: "/Applications/Clash Verge.app/Contents/MacOS/clash-verge"}, Foreign, ClientVerge},
		{"path unreadable, nothing known", &Process{PID: 9}, Unknown, ""},
	} {
		o := Classify("127.0.0.1:9090", tc.p, exp, now)
		if o.Verdict != tc.want || o.Client != tc.cl {
			t.Errorf("%s: %s %q, want %s %q", tc.name, o.Verdict, o.Client, tc.want, tc.cl)
		}
		if o.Addr != "127.0.0.1:9090" || !o.At.Equal(now) {
			t.Errorf("%s: %+v", tc.name, o)
		}
	}
	// Windows paths compare without case.
	if o := Classify("a", &Process{PID: 3, Exe: `c:\programdata\twinnel\kernels\mihomo\current\mihomo.exe`}, Expect{Dirs: []string{`C:\ProgramData\Twinnel\kernels\mihomo`}}, now); o.Verdict != Managed {
		t.Errorf("windows case: %s", o.Verdict)
	}
}

func TestLabel(t *testing.T) {
	o := Owner{Addr: "127.0.0.1:9090", Verdict: Foreign, Client: ClientClashParty, PID: 4242}
	if l := o.Label(); l != "127.0.0.1:9090 (Clash Party, pid 4242)" {
		t.Error(l)
	}
	if l := (Owner{Addr: "a:1", Verdict: Managed}).Label(); l != "a:1 (Twinnel)" {
		t.Error(l)
	}
	if l := (Owner{Addr: "a:1", Verdict: Foreign}).Label(); l != "a:1 (another mihomo)" {
		t.Error(l)
	}
	var nilOwner *Owner
	if nilOwner.IsForeign() {
		t.Error("nil is foreign")
	}
}

// The report's error (Appendix A), with synthetic paths.
const safePathsErr = "reload: PUT /configs?force=true: 400 Bad Request: path is not subpath of home directory or SAFE_PATHS: C:\\ProgramData\\Twinnel\\mihomo\\config.yaml\n allowed paths: [C:\\Users\\someone\\AppData\\Roaming\\mihomo-party\\work]"

func TestSafePaths(t *testing.T) {
	if !IsSafePathsError(safePathsErr) {
		t.Fatal("not recognised")
	}
	if IsSafePathsError("reload: PUT /configs: 400 Bad Request: yaml: line 3: bad") {
		t.Fatal("other 400 recognised")
	}
	if got := SafePathsAllowed(safePathsErr); !slices.Equal(got, []string{`C:\Users\someone\AppData\Roaming\mihomo-party\work`}) {
		t.Errorf("allowed = %q", got)
	}
	two := "path is not subpath of home directory or SAFE_PATHS: /a/config.yaml\n allowed paths: [/Users/x/Library/Application Support/io.github.clash-verge-rev.clash-verge-rev /tmp/y]"
	if got := SafePathsAllowed(two); !slices.Equal(got, []string{"/Users/x/Library/Application Support/io.github.clash-verge-rev.clash-verge-rev", "/tmp/y"}) {
		t.Errorf("two = %q", got)
	}
	o := FromSafePaths("127.0.0.1:9090", safePathsErr, time.Unix(1, 0))
	if o.Verdict != Foreign || o.Client != ClientClashParty || o.Via != ViaSafePaths || !strings.Contains(o.WorkDir, "mihomo-party") || o.Detail != safePathsErr {
		t.Errorf("%+v", o)
	}
}

func TestPort(t *testing.T) {
	for in, want := range map[string]int{"127.0.0.1:9090": 9090, ":19894": 19894, "http://127.0.0.1:9097/": 9097, "[::1]:9090": 9090, "nope": 0, "h:0": 0} {
		if got := Port(in); got != want {
			t.Errorf("Port(%q) = %d", in, got)
		}
	}
}

func TestLsofPortOwner(t *testing.T) {
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch {
		case name == "/usr/sbin/lsof" && slices.Contains(args, "-sTCP:LISTEN"):
			return []byte("p5150\nf9\n"), nil
		case name == "/usr/sbin/lsof":
			return []byte("p5150\nfcwd\nn/Users/someone/Library/Application Support/mihomo-party/work\n"), nil
		case slices.Contains(args, "args="):
			return []byte("/Applications/Clash Party.app/Contents/Resources/sidecar/mihomo -d /Users/someone/Library/Application\n"), nil
		case slices.Contains(args, "5150"):
			return []byte("  812 /Applications/Clash Party.app/Contents/Resources/sidecar/mihomo\n"), nil
		case slices.Contains(args, "812"):
			return []byte("    1 /Applications/Clash Party.app/Contents/MacOS/Clash Party\n"), nil
		}
		return nil, errors.New("unexpected")
	}
	p, err := LsofPortOwner(context.Background(), run, 9090)
	if err != nil || p == nil {
		t.Fatal(p, err)
	}
	if p.PID != 5150 || p.PPID != 812 || !strings.HasSuffix(p.Exe, "sidecar/mihomo") || !strings.HasSuffix(p.ParentExe, "MacOS/Clash Party") || !strings.HasSuffix(p.Cwd, "mihomo-party/work") {
		t.Errorf("%+v", p)
	}
	if IdentifyProcess(*p) != ClientClashParty {
		t.Error("not Clash Party")
	}
	// Nothing listens: lsof exits 1 with no output.
	none := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("exit status 1") }
	if p, err := LsofPortOwner(context.Background(), none, 9090); p != nil || err != nil {
		t.Error(p, err)
	}
}

func TestProcPortOwner(t *testing.T) {
	root := t.TempDir()
	write := func(rel, s string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, rel string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
	}
	// 9090 = 0x2382, LISTEN = 0A; a connected socket on the same port
	// (state 01) must not count.
	write("net/tcp", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"+
		"   0: 0100007F:2382 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 4711 1 0000000000000000 100 0 0 10 0\n"+
		"   1: 0100007F:2382 0100007F:D431 01 00000000:00000000 00:00000000 00000000  1000        0 4800 1 0000000000000000 20 4 30 10 -1\n")
	write("net/tcp6", "  sl  local_address remote_address st\n")
	write("300/stat", "300 (mihomo party) S 299 300 300 0 -1")
	write("300/cmdline", "/opt/clash-party/resources/sidecar/mihomo\x00-d\x00/home/someone/.config/mihomo-party/work\x00")
	link("/opt/clash-party/resources/sidecar/mihomo", "300/exe")
	link("/home/someone/.config/mihomo-party/work", "300/cwd")
	link("socket:[4800]", "300/fd/7")
	link("socket:[4711]", "300/fd/8")
	link("/opt/clash-party/clash-party", "299/exe")
	write("299/stat", "299 (clash-party) S 1 1 1")
	link("socket:[1]", "12/fd/3")

	p, err := ProcPortOwner(context.Background(), root, 9090)
	if err != nil || p == nil {
		t.Fatal(p, err)
	}
	if p.PID != 300 || p.PPID != 299 || p.ParentExe != "/opt/clash-party/clash-party" || WorkDir(*p) != "/home/someone/.config/mihomo-party/work" {
		t.Errorf("%+v", p)
	}
	if p, err := ProcPortOwner(context.Background(), root, 9091); p != nil || err != nil {
		t.Error("port 9091:", p, err)
	}
}

func TestParsers(t *testing.T) {
	if pp, c := ParsePSPPIDComm("  1 /Applications/A B.app/x\n"); pp != 1 || c != "/Applications/A B.app/x" {
		t.Error(pp, c)
	}
	if n := ProcStatPPID("42 (a) b) R 7 1 1"); n != 7 {
		t.Error(n)
	}
	if got := ProcCmdline([]byte("a\x00b c\x00")); !slices.Equal(got, []string{"a", "b c"}) {
		t.Error(got)
	}
	if got := ParseLsofPIDs("p1\nf2\np1\np3\n"); !slices.Equal(got, []int{1, 3}) {
		t.Error(got)
	}
}
