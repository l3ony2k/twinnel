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
	"time"

	"github.com/l3ony2k/twinnel/core/service"
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
	spec := service.MihomoSpec("com.example.mihomo", "/opt/twinnel/kernels/mihomo/current/mihomo", "/opt/twinnel/mihomo", "/opt/twinnel/mihomo/config.yaml", "/opt/twinnel/logs")
	spec.Env = map[string]string{"SAFE_PATHS": "/a & <b>"}
	b, err := l.Render(spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<key>Label</key>\n\t<string>com.example.mihomo</string>",
		"<string>/opt/twinnel/kernels/mihomo/current/mihomo</string>\n\t\t<string>-d</string>\n\t\t<string>/opt/twinnel/mihomo</string>\n\t\t<string>-f</string>",
		"<key>KeepAlive</key>\n\t<true/>",
		"<key>ThrottleInterval</key>\n\t<integer>10</integer>",
		"<string>/opt/twinnel/logs/mihomo.err.log</string>",
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
	spec := service.HelperSpec("com.example.helper", "/Library/PrivilegedHelperTools/com.example.helper", "/x/state", "/var/run/twinnel/helper.sock", "/x/state/logs")

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
		"wait until `/bin/launchctl print system/com.example.helper` says \"Could not find service\"",
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

	noSleep(t)
	// Apply with fakes: the plist lands in the temp dir, launchctl is never run.
	ex := &fakeExec{}
	ex.answer("/bin/launchctl bootout system/com.example.helper", []byte("Boot-out failed: 3: No such process"), errors.New("exit status 3"))
	ex.answer("/bin/launchctl print system/com.example.helper", []byte(`Could not find service "com.example.helper" in domain for system`), errors.New("exit status 113"))
	files := &memFiles{m: map[string][]byte{}}
	if err := service.Apply(context.Background(), steps, ex, files); err != nil {
		t.Fatal(err)
	}
	if _, ok := files.m[dir+"/com.example.helper.plist"]; !ok {
		t.Error("plist not written")
	}
	if len(ex.calls) != 4 {
		t.Errorf("calls = %v", ex.calls)
	}

	// A bootstrap that keeps failing stops the plan, loudly, after the
	// retries.
	ex = &fakeExec{}
	ex.answer("/bin/launchctl print system/com.example.helper", []byte(`Could not find service "com.example.helper"`), errors.New("exit status 113"))
	ex.answer("/bin/launchctl bootstrap system "+dir+"/com.example.helper.plist", []byte("Bootstrap failed: 5: Input/output error"), errors.New("exit status 5"))
	err = service.Apply(context.Background(), steps, ex, files)
	if err == nil || !strings.Contains(err.Error(), "Input/output error") || !strings.Contains(err.Error(), "after 5 attempts") {
		t.Errorf("bootstrap failure: %v", err)
	}
	if n := countCalls(ex, "bootstrap"); n != 5 {
		t.Errorf("bootstrap ran %d times", n)
	}
	// Any other bootstrap error is not retried.
	ex = &fakeExec{}
	ex.answer("/bin/launchctl print system/com.example.helper", []byte(`Could not find service "com.example.helper"`), errors.New("exit status 113"))
	ex.answer("/bin/launchctl bootstrap system "+dir+"/com.example.helper.plist", []byte("Bootstrap failed: 22: Invalid argument"), errors.New("exit status 22"))
	if err := service.Apply(context.Background(), steps, ex, files); err == nil || countCalls(ex, "bootstrap") != 1 {
		t.Errorf("error 22: %v, %v", err, ex.calls)
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

func noSleep(t *testing.T) {
	old := service.Sleep
	service.Sleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { service.Sleep = old })
}

func countCalls(ex *fakeExec, verb string) int {
	n := 0
	for _, c := range ex.calls {
		if len(c) > 1 && c[1] == verb {
			n++
		}
	}
	return n
}

// fakeLaunchd models the system domain for one label: bootout is
// asynchronous (the label stays known for goneAfter more prints), and a
// bootstrap while it is still known fails with 37, as launchd does.
type fakeLaunchd struct {
	label, plist string
	loaded       bool
	going        int // prints left before a booted-out label is gone
	goneAfter    int
	failBoot     []string // errors the next bootstraps return, in turn
	pid          int
}

func (f *fakeLaunchd) exec() *fakeExec {
	return &fakeExec{hook: func(argv string) ([]byte, error, bool) {
		target := "system/" + f.label
		switch argv {
		case "/bin/launchctl print " + target:
			if f.going > 0 {
				f.going--
				if f.going == 0 {
					f.loaded = false
				}
				return []byte(target + " = {\n\tstate = exiting\n}\n"), nil, true
			}
			if !f.loaded {
				return []byte(`Could not find service "` + f.label + `" in domain for system`), errors.New("exit status 113"), true
			}
			return []byte(target + " = {\n\tstate = running\n}\n"), nil, true
		case "/bin/launchctl bootout " + target:
			if !f.loaded {
				return []byte("Boot-out failed: 3: No such process"), errors.New("exit status 3"), true
			}
			f.going = f.goneAfter
			if f.going == 0 {
				f.loaded = false
			}
			return nil, nil, true
		case "/bin/launchctl bootstrap system " + f.plist:
			if len(f.failBoot) > 0 {
				e := f.failBoot[0]
				f.failBoot = f.failBoot[1:]
				return []byte("Bootstrap failed: " + e), errors.New("exit status " + strings.SplitN(e, ":", 2)[0]), true
			}
			if f.loaded {
				return []byte("Bootstrap failed: 37: Operation already in progress"), errors.New("exit status 37"), true
			}
			f.loaded, f.pid = true, f.pid+1
			return nil, nil, true
		case "/bin/launchctl kickstart -k " + target:
			if !f.loaded {
				return []byte(`Could not find service "` + f.label + `" in domain for system`), errors.New("exit status 113"), true
			}
			f.pid++
			return nil, nil, true
		case "/bin/launchctl enable " + target:
			return nil, nil, true
		}
		return []byte("unexpected " + argv), errors.New("exit status 1"), true
	}}
}

func TestInstallWaitsForAsyncBootout(t *testing.T) {
	noSleep(t)
	dir := t.TempDir()
	l := &Launchd{Dir: dir}
	spec := service.HelperSpec("com.example.helper", "/x/helper", "/x/state", "/x/s.sock", "/x/state/logs")
	steps, err := l.Plan(service.OpInstall, spec)
	if err != nil {
		t.Fatal(err)
	}
	fl := &fakeLaunchd{label: spec.Label, plist: dir + "/com.example.helper.plist", loaded: true, goneAfter: 3}
	ex := fl.exec()
	if err := service.Apply(context.Background(), steps, ex, &memFiles{m: map[string][]byte{}}); err != nil {
		t.Fatalf("%v\n%v", err, ex.calls)
	}
	if !fl.loaded || countCalls(ex, "bootstrap") != 1 || countCalls(ex, "print") != 4 {
		t.Errorf("loaded %v, calls %v", fl.loaded, ex.calls)
	}

	// Without the wait the same plan fails as on the owner's Mac: the
	// bootstrap lands while launchd still knows the label.
	var noWait []service.Step
	for _, s := range steps {
		if s.Kind != service.StepWait {
			noWait = append(noWait, s)
		}
	}
	fl = &fakeLaunchd{label: spec.Label, plist: dir + "/com.example.helper.plist", loaded: true, goneAfter: 100}
	noWait[len(noWait)-1].Retry = 0
	if err := service.Apply(context.Background(), noWait, fl.exec(), &memFiles{m: map[string][]byte{}}); err == nil {
		t.Error("the race does not show without the wait")
	}
}

func TestInstallRetriesBootstrap(t *testing.T) {
	noSleep(t)
	dir := t.TempDir()
	l := &Launchd{Dir: dir}
	spec := service.HelperSpec("com.example.helper", "/x/helper", "/x/state", "/x/s.sock", "/x/state/logs")
	steps, _ := l.Plan(service.OpInstall, spec)
	fl := &fakeLaunchd{label: spec.Label, plist: dir + "/com.example.helper.plist", loaded: true,
		failBoot: []string{"5: Input/output error", "37: Operation already in progress"}}
	ex := fl.exec()
	if err := service.Apply(context.Background(), steps, ex, &memFiles{m: map[string][]byte{}}); err != nil {
		t.Fatal(err)
	}
	if !fl.loaded || countCalls(ex, "bootstrap") != 3 {
		t.Errorf("loaded %v, calls %v", fl.loaded, ex.calls)
	}
}

func TestPlanReload(t *testing.T) {
	noSleep(t)
	dir := t.TempDir()
	l := &Launchd{Dir: dir}
	spec := service.HelperSpec("com.example.helper", "/x/helper", "/x/state", "/x/s.sock", "/x/state/logs")
	current, _ := l.Render(spec)

	// Same plist, loaded: kickstart only.
	steps, err := l.PlanReload(spec, current, true)
	if err != nil || len(steps) != 1 || steps[0].String() != "/bin/launchctl kickstart -k system/com.example.helper" {
		t.Fatalf("%v %v", steps, err)
	}
	fl := &fakeLaunchd{label: spec.Label, plist: dir + "/com.example.helper.plist", loaded: true, pid: 5}
	ex := fl.exec()
	if err := service.Apply(context.Background(), steps, ex, &memFiles{m: map[string][]byte{}}); err != nil || fl.pid != 6 || len(ex.calls) != 1 {
		t.Errorf("%v pid %d calls %v", err, fl.pid, ex.calls)
	}

	// Another plist, or not loaded: the full plan.
	for _, c := range []struct {
		cur    []byte
		loaded bool
	}{{[]byte("<old/>"), true}, {current, false}, {nil, false}} {
		steps, _ := l.PlanReload(spec, c.cur, c.loaded)
		if len(steps) < 5 || steps[len(steps)-1].Argv[1] != "bootstrap" {
			t.Errorf("%s/%v: %v", c.cur, c.loaded, steps)
		}
	}
}

func TestPlanStartOrRestart(t *testing.T) {
	noSleep(t)
	dir := t.TempDir()
	l := &Launchd{Dir: dir}
	steps, _ := l.PlanStartOrRestart("com.example.helper", true)
	if len(steps) != 1 || steps[0].Argv[1] != "kickstart" {
		t.Errorf("loaded: %v", steps)
	}
	// Not loaded (an upgrade that unloaded it): kickstart cannot work;
	// enable and bootstrap do.
	steps, _ = l.PlanStartOrRestart("com.example.helper", false)
	fl := &fakeLaunchd{label: "com.example.helper", plist: dir + "/com.example.helper.plist", failBoot: []string{"5: Input/output error"}}
	ex := fl.exec()
	if err := service.Apply(context.Background(), steps, ex, &memFiles{m: map[string][]byte{}}); err != nil || !fl.loaded {
		t.Errorf("not loaded: %v %v", err, ex.calls)
	}
	if _, err := l.PlanStartOrRestart("../x", false); err == nil {
		t.Error("bad label accepted")
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
