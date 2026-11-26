//go:build darwin

package launchd

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/service"
)

// fakeExec records commands and answers from a table.
type fakeExec struct {
	calls   [][]string
	answers map[string]struct {
		out []byte
		err error
	}
	// hook, when set, answers first (ok=true) and may keep state.
	hook func(argv string) (out []byte, err error, ok bool)
}

func (f *fakeExec) Run(ctx context.Context, argv []string) ([]byte, error) {
	f.calls = append(f.calls, argv)
	if f.hook != nil {
		if out, err, ok := f.hook(strings.Join(argv, " ")); ok {
			return out, err
		}
	}
	a := f.answers[strings.Join(argv, " ")]
	return a.out, a.err
}

func (f *fakeExec) answer(cmd string, out []byte, err error) {
	if f.answers == nil {
		f.answers = map[string]struct {
			out []byte
			err error
		}{}
	}
	f.answers[cmd] = struct {
		out []byte
		err error
	}{out, err}
}

func TestRenderPlist(t *testing.T) {
	l := &Launchd{Dir: t.TempDir()}
	spec := service.MihomoSpec("com.example.mihomo", "/opt/duet/kernels/mihomo/current/mihomo", "/opt/duet/mihomo", "/opt/duet/mihomo/config.yaml", "/opt/duet/logs")
	spec.Env = map[string]string{"SAFE_PATHS": "/a & <b>"}
	b, err := l.Render(spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<key>Label</key>\n\t<string>com.example.mihomo</string>",
		"<string>/opt/duet/kernels/mihomo/current/mihomo</string>\n\t\t<string>-d</string>\n\t\t<string>/opt/duet/mihomo</string>\n\t\t<string>-f</string>",
		"<key>KeepAlive</key>\n\t<true/>",
		"<key>ThrottleInterval</key>\n\t<integer>10</integer>",
		"<string>/opt/duet/logs/mihomo.err.log</string>",
		"<string>/a &amp; &lt;b&gt;</string>",
		"<key>UserName</key>\n\t<string>root</string>",
	} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("plist lacks %q:\n%s", want, b)
		}
	}
	// plutil -lint is read-only and validates the XML against the plist format.
	if pl, err := exec.LookPath("plutil"); err == nil {
		f := filepath.Join(t.TempDir(), "x.plist")
		os.WriteFile(f, b, 0o644)
		if out, err := exec.Command(pl, "-lint", f).CombinedOutput(); err != nil {
			t.Errorf("plutil -lint: %v: %s", err, out)
		}
	}
	if _, err := l.Render(service.Spec{Label: "bad label/../x", Program: "/x"}); err == nil {
		t.Error("bad label accepted")
	}
	if _, err := l.Render(service.Spec{Label: "ok", Program: "relative"}); err == nil {
		t.Error("relative program accepted")
	}
}

func TestPlans(t *testing.T) {
	dir := t.TempDir()
	l := &Launchd{Dir: dir, Launchctl: "/bin/launchctl"}
	spec := service.HelperSpec("com.example.helper", "/Library/PrivilegedHelperTools/com.example.helper", "/x/state", "/var/run/duet/helper.sock", "/x/state/logs")

	steps, err := l.Plan(service.OpInstall, spec)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, s := range steps {
		lines = append(lines, s.String())
	}
	want := []string{
		"install -d -m 0755 /x/state/logs",
		"/bin/launchctl bootout system/com.example.helper  # errors ignored",
		"write " + dir + "/com.example.helper.plist (mode 0644, ",
		"/bin/launchctl enable system/com.example.helper",
		"/bin/launchctl bootstrap system " + dir + "/com.example.helper.plist",
	}
	if len(lines) != len(want) {
		t.Fatalf("install plan:\n%s", strings.Join(lines, "\n"))
	}
	for i := range want {
		if !strings.HasPrefix(lines[i], want[i]) {
			t.Errorf("step %d = %q, want prefix %q", i, lines[i], want[i])
		}
	}

	// Apply with fakes: the plist lands in the temp dir, launchctl is never run.
	ex := &fakeExec{}
	ex.answer("/bin/launchctl bootout system/com.example.helper", []byte("Boot-out failed: 3: No such process"), errors.New("exit status 3"))
	files := &memFiles{m: map[string][]byte{}}
	if err := service.Apply(context.Background(), steps, ex, files); err != nil {
		t.Fatal(err)
	}
	if _, ok := files.m[dir+"/com.example.helper.plist"]; !ok {
		t.Error("plist not written")
	}
	if len(ex.calls) != 3 {
		t.Errorf("calls = %v", ex.calls)
	}

	// A failing bootstrap stops the plan.
	ex = &fakeExec{}
	ex.answer("/bin/launchctl bootstrap system "+dir+"/com.example.helper.plist", []byte("Bootstrap failed: 5: Input/output error"), errors.New("exit status 5"))
	err = service.Apply(context.Background(), steps, ex, files)
	if err == nil || !strings.Contains(err.Error(), "Input/output error") {
		t.Errorf("bootstrap failure: %v", err)
	}
	if len(ex.calls) != 3 {
		t.Errorf("ran past the failure: %v", ex.calls)
	}

	// A label disabled earlier: launchd refuses bootstrap until it is
	// enabled, so the plan enables first and the bootstrap succeeds.
	disabled := true
	ex = &fakeExec{hook: func(argv string) ([]byte, error, bool) {
		switch argv {
		case "/bin/launchctl enable system/com.example.helper":
			disabled = false
		case "/bin/launchctl bootstrap system " + dir + "/com.example.helper.plist":
			if disabled {
				return []byte("Bootstrap failed: 5: Input/output error"), errors.New("exit status 5"), true
			}
		}
		return nil, nil, false
	}}
	if err := service.Apply(context.Background(), steps, ex, files); err != nil {
		t.Errorf("disabled label: %v", err)
	}

	for op, first := range map[service.Op]string{
		service.OpUninstall: "/bin/launchctl bootout system/com.example.helper  # errors ignored",
		service.OpStart:     "/bin/launchctl bootstrap system " + dir + "/com.example.helper.plist  # errors ignored",
		service.OpStop:      "/bin/launchctl bootout system/com.example.helper",
		service.OpRestart:   "/bin/launchctl kickstart -k system/com.example.helper",
		service.OpDisable:   "/bin/launchctl disable system/com.example.helper",
		service.OpEnable:    "/bin/launchctl enable system/com.example.helper",
	} {
		steps, err := l.Plan(op, service.Spec{Label: "com.example.helper"})
		if err != nil || steps[0].String() != first {
			t.Errorf("%s: %v %v", op, steps, err)
		}
	}
	if _, err := l.Plan("explode", spec); err == nil {
		t.Error("unknown op accepted")
	}
}

func TestStatus(t *testing.T) {
	running, _ := os.ReadFile("testdata/print-running.txt")
	exited, _ := os.ReadFile("testdata/print-exited.txt")
	ex := &fakeExec{}
	l := &Launchd{Dir: "/Library/LaunchDaemons", Exec: ex, Stat: func(string) (fs.FileInfo, error) { return nil, fs.ErrNotExist }}

	ex.answer("/bin/launchctl print system/com.example.mihomo", running, nil)
	st, err := l.Status(context.Background(), "com.example.mihomo")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Loaded || !st.Running || st.PID != 4242 || st.Runs != 2 || st.LastExitCode == nil || *st.LastExitCode != 0 ||
		st.Program != "/opt/example/bin/mihomo" || !st.Installed {
		t.Errorf("running: %+v", st)
	}

	ex.answer("/bin/launchctl print system/com.example.mihomo", exited, nil)
	st, _ = l.Status(context.Background(), "com.example.mihomo")
	if !st.Loaded || st.Running || st.PID != 0 || *st.LastExitCode != 1 || st.Runs != 7 {
		t.Errorf("exited: %+v", st)
	}

	ex.answer("/bin/launchctl print system/com.example.none", []byte("Bad request.\nCould not find service \"com.example.none\" in domain for system\n"), errors.New("exit status 113"))
	st, err = l.Status(context.Background(), "com.example.none")
	if err != nil || st.Loaded || st.Installed {
		t.Errorf("missing: %+v %v", st, err)
	}

	ex.answer("/bin/launchctl print system/com.example.err", []byte("boom"), errors.New("exit status 1"))
	if _, err := l.Status(context.Background(), "com.example.err"); err == nil {
		t.Error("launchctl failure not reported")
	}
	if _, err := l.Status(context.Background(), "../x"); err == nil {
		t.Error("bad label accepted")
	}
}

func TestParsePrintIgnoresNested(t *testing.T) {
	m := ParsePrint([]byte("system/x = {\n\tstate = running\n\tres = {\n\t\tstate = active\n\t}\n}\n"))
	if m["state"] != "running" || len(m) != 1 {
		t.Errorf("%v", m)
	}
}

type memFiles struct{ m map[string][]byte }

func (f *memFiles) MkdirAll(string, fs.FileMode) error { return nil }
func (f *memFiles) WriteFile(p string, b []byte, _ fs.FileMode) error {
	f.m[p] = b
	return nil
}
func (f *memFiles) Copy(src, dst string, _ fs.FileMode) error {
	f.m[dst] = []byte("copy of " + src)
	return nil
}
func (f *memFiles) Remove(p string) error { delete(f.m, p); return nil }
