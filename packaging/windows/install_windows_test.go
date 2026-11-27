//go:build windows

package windows

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/l3ony2k/twinnel/platform/windows/userpath"
)

// TestInstallScriptEndToEnd runs the whole of install.ps1, the way users
// and the app run it, under Windows PowerShell 5.1 and PowerShell 7:
// install, upgrade while Twinnel's programs run from the install, dry
// runs of both, and the uninstall the app's 完全移除 starts. The programs
// are stand-ins (testdata/fakeexe) built twice, as two versions.
//
// It changes the signed-in user's PATH and Start menu (and puts them
// back), so it runs only where TWINNEL_INSTALL_E2E=1 (the Windows CI job)
// and only on a machine without Twinnel.
func TestInstallScriptEndToEnd(t *testing.T) {
	if os.Getenv("TWINNEL_INSTALL_E2E") != "1" {
		t.Skip("set TWINNEL_INSTALL_E2E=1 to run install.ps1 end to end (changes this user's PATH and Start menu; CI only)")
	}
	if why := twinnelPresent(); why != "" {
		t.Skipf("Twinnel is on this machine (%s): not running the installer over it", why)
	}
	if n := len(shells()); n < 2 {
		t.Fatalf("found %d PowerShell(s) (%v); the end-to-end run needs both powershell.exe and pwsh", n, shells())
	}
	bins := t.TempDir()
	v1, v2 := buildFake(t, bins, "v1"), buildFake(t, bins, "v2")
	eachShell(t, func(t *testing.T, sh string) {
		e := newInstallEnv(t, sh, v1, v2)
		e.install()
		e.upgradeWhileRunning()
		e.dryRuns()
		e.uninstall()
	})
}

// twinnelPresent names what of a real Twinnel install the script's
// uninstall would touch, "" when there is none.
func twinnelPresent() string {
	if _, err := os.Stat(shortcutPath()); err == nil {
		return shortcutPath()
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("ProgramFiles"), `Twinnel\twinnel-helper.exe`)); err == nil {
		return "the helper"
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if _, _, err := k.GetStringValue("io.github.l3ony2k.twinnel"); err == nil {
			return "the start-at-login entry"
		}
	}
	return ""
}

func shortcutPath() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\Twinnel.lnk`)
}

// buildFake builds testdata/fakeexe reporting version v.
func buildFake(t *testing.T, dir, v string) string {
	t.Helper()
	out := filepath.Join(dir, "fake-"+v+".exe")
	cmd := exec.Command("go", "build", "-o", out, "-ldflags", "-X main.version="+v, "./testdata/fakeexe")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake: %v\n%s", err, b)
	}
	return out
}

// longPath: the 8.3-free form of p (the runner's %TEMP% has a short
// name), as Windows reports the path of a running program.
func longPath(t *testing.T, p string) string {
	t.Helper()
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetLongPathName(u, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		t.Fatalf("long path of %s: %v", p, err)
	}
	return windows.UTF16ToString(buf[:n])
}

type installEnv struct {
	t        *testing.T
	sh       string
	src1     string // unpacked zip, version v1
	src2     string // unpacked zip, version v2
	dir      string // InstallDir
	bin      string
	scratch  string
	held     []*heldProc
	snapshot map[string]string
}

// heldProc is a program started from the install that keeps its file
// open, as an agent's `twinnel mcp` does.
type heldProc struct {
	name string
	cmd  *exec.Cmd
	done chan struct{}
}

func (h *heldProc) running() bool {
	select {
	case <-h.done:
		return false
	default:
		return true
	}
}

func newInstallEnv(t *testing.T, sh, v1, v2 string) *installEnv {
	root := longPath(t, t.TempDir())
	e := &installEnv{t: t, sh: sh, dir: filepath.Join(root, "Twinnel"), scratch: filepath.Join(root, "scratch")}
	e.bin = filepath.Join(e.dir, "bin")
	e.src1 = e.unpack(filepath.Join(root, "zip-v1"), v1)
	e.src2 = e.unpack(filepath.Join(root, "zip-v2"), v2)
	if err := os.MkdirAll(e.scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	// Registered after the temp dirs, so it runs before they are removed:
	// nothing of ours keeps running, and the user's PATH and Start menu
	// lose what a failed run left.
	t.Cleanup(func() {
		for _, h := range e.held {
			_ = h.cmd.Process.Kill()
		}
		q := strings.ReplaceAll(e.dir, "'", "''")
		_ = exec.Command(sh, "-NoProfile", "-NonInteractive", "-Command",
			"Get-Process | Where-Object { $_.Path -and $_.Path.StartsWith('"+q+`\', 'OrdinalIgnoreCase') } | Stop-Process -Force`).Run()
		if t.Failed() {
			_, _ = userpath.RemoveDir(e.bin)
			_ = os.Remove(shortcutPath())
		}
	})
	return e
}

// unpack lays out a release zip's contents: the programs and install.ps1.
func (e *installEnv) unpack(dir, fake string) string {
	b, err := os.ReadFile(fake)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, name := range []string{"Twinnel.exe", `bin\twinnel.exe`, `bin\twinnel-helper.exe`} {
		write(e.t, filepath.Join(dir, name), string(b))
	}
	write(e.t, filepath.Join(dir, "install.ps1"), read(e.t, "install.ps1"))
	write(e.t, filepath.Join(dir, "README.md"), "readme")
	return dir
}

// run runs a script file the way a user does (-File), returns its output
// and exit code. The output goes to a file, not a pipe: a program the
// script starts may inherit the handle and would hold a pipe open.
func (e *installEnv) run(script string, args ...string) (string, int) {
	e.t.Helper()
	f, err := os.CreateTemp(e.scratch, "out-*.txt")
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	cmd := exec.Command(e.sh, append([]string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script}, args...)...)
	cmd.Dir = e.scratch
	cmd.Stdout, cmd.Stderr = f, f
	err = cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			e.t.Fatalf("run %s: %v", script, err)
		}
		code = ee.ExitCode()
	}
	out := strings.ReplaceAll(read(e.t, f.Name()), "\r\n", "\n")
	e.t.Logf("%s %s → exit %d\n%s", filepath.Base(script), strings.Join(args, " "), code, out)
	return out, code
}

func (e *installEnv) hold(rel string) *heldProc {
	e.t.Helper()
	cmd := exec.Command(filepath.Join(e.dir, rel), "hold")
	cmd.Dir = e.scratch
	if err := cmd.Start(); err != nil {
		e.t.Fatal(err)
	}
	h := &heldProc{name: rel, cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(h.done) }()
	e.held = append(e.held, h)
	return h
}

func (e *installEnv) wantOut(out string, res ...string) {
	e.t.Helper()
	for _, re := range res {
		if !regexp.MustCompile(re).MatchString(out) {
			e.t.Errorf("output lacks /%s/", re)
		}
	}
}

func (e *installEnv) noOut(out string, res ...string) {
	e.t.Helper()
	for _, re := range res {
		if regexp.MustCompile(re).MatchString(out) {
			e.t.Errorf("output has /%s/", re)
		}
	}
}

// wantVersion: every installed program is the build of version v, and the
// marker names it.
func (e *installEnv) wantVersion(v string, fake string) {
	e.t.Helper()
	want := read(e.t, fake)
	for _, name := range []string{"Twinnel.exe", `bin\twinnel.exe`, `bin\twinnel-helper.exe`} {
		if read(e.t, filepath.Join(e.dir, name)) != want {
			e.t.Errorf("%s is not %s", name, v)
		}
	}
	if m := strings.TrimSpace(read(e.t, filepath.Join(e.dir, ".twinnel-install"))); m != v {
		e.t.Errorf("marker says %q, want %q", m, v)
	}
}

func (e *installEnv) onPath() bool {
	e.t.Helper()
	ok, err := userpath.Has(e.bin)
	if err != nil {
		e.t.Fatal(err)
	}
	return ok
}

func (e *installEnv) install() {
	t := e.t
	// A wrong folder is refused before anything happens.
	out, code := e.run(filepath.Join(e.src1, "install.ps1"), "-InstallDir", `C:\`, "-NoLaunch")
	if code == 0 {
		t.Fatal("InstallDir C:\\ accepted")
	}
	e.wantOut(out, `InstallDir must be`)

	out, code = e.run(filepath.Join(e.src1, "install.ps1"), "-InstallDir", e.dir, "-NoLaunch")
	if code != 0 {
		t.Fatalf("install: exit %d", code)
	}
	e.wantOut(out, `(?m)^copy Twinnel to `, `(?m)^add .*bin to the user's PATH`, `(?m)^Twinnel is installed in `)
	e.noOut(out, `was in use`, `(?m)^start Twinnel`, `(?i)exception|argument types`)
	e.wantVersion("v1", filepath.Join(e.src1, "Twinnel.exe"))
	if read(t, filepath.Join(e.dir, "README.md")) != "readme" {
		t.Error("README.md not installed")
	}
	if !e.onPath() {
		t.Error("bin not on the user's PATH")
	}
	if _, err := os.Stat(shortcutPath()); err != nil {
		t.Errorf("no Start menu shortcut: %v", err)
	}
}

// The QA round-2 upgrade: the app runs in this session and two programs
// run from bin\ (an agent's `twinnel mcp`, a CLI). The app is stopped and
// started again; the others keep running from their renamed-aside files
// and are named one per line.
func (e *installEnv) upgradeWhileRunning() {
	t := e.t
	app := e.hold("Twinnel.exe")
	cli := e.hold(`bin\twinnel.exe`)
	helper := e.hold(`bin\twinnel-helper.exe`)
	out, code := e.run(filepath.Join(e.src2, "install.ps1"), "-InstallDir", e.dir, "-NoLaunch")
	if code != 0 {
		t.Fatalf("upgrade: exit %d", code)
	}
	e.wantOut(out,
		`(?m)^stop Twinnel\.exe \(pid \d+\)`,
		`(?m)^  twinnel\.exe was in use: `,
		`(?m)^  twinnel-helper\.exe was in use: `,
		`(?m)^start Twinnel again`,
		`(?m)^Twinnel is installed in `)
	e.noOut(out, `add .* to the user's PATH`, `(?i)exception|argument types`)
	e.wantVersion("v2", filepath.Join(e.src2, "Twinnel.exe"))
	select {
	case <-app.done:
	case <-time.After(10 * time.Second):
		t.Error("the app was not stopped")
	}
	for _, h := range []*heldProc{cli, helper} {
		if !h.running() {
			t.Errorf("%s stopped; it should keep running from its renamed file", h.name)
		}
	}
	old, _ := filepath.Glob(filepath.Join(e.bin, "*.old-*"))
	if len(old) != 2 {
		t.Errorf("renamed-aside files %v, want twinnel.exe's and twinnel-helper.exe's", old)
	}
	if !e.onPath() {
		t.Error("bin left the user's PATH")
	}
}

func (e *installEnv) dryRuns() {
	t := e.t
	// A program started from the new file is in use now.
	e.hold(`bin\twinnel.exe`)
	before := e.tree()
	out, code := e.run(filepath.Join(e.src1, "install.ps1"), "-InstallDir", e.dir, "-NoLaunch", "-DryRun")
	if code != 0 {
		t.Fatalf("install -DryRun: exit %d", code)
	}
	e.wantOut(out,
		`(?m)^would: stop Twinnel\.exe \(pid \d+\)`,
		`(?m)^would: copy Twinnel to `,
		`(?m)^  twinnel\.exe is in use: it would be renamed aside`,
		`(?m)^would: start Twinnel again`,
		`(?m)^Dry run: nothing was changed\.`)
	e.noOut(out, `Twinnel\.exe is in use`, `(?m)^Twinnel is installed`, `(?i)exception|argument types`)

	out, code = e.run(filepath.Join(e.dir, "install.ps1"), "-Uninstall", "-InstallDir", e.dir, "-DryRun")
	if code != 0 {
		t.Fatalf("uninstall -DryRun: exit %d", code)
	}
	e.wantOut(out,
		`(?m)^would: stop Twinnel\.exe \(pid \d+\), it runs from the install folder`,
		`(?m)^would: stop twinnel\.exe.* \(pid \d+\), it runs from the install folder`,
		`(?m)^would: remove .*Twinnel\.lnk`,
		`(?m)^would: remove .*bin from the user's PATH`,
		`(?m)^would: remove `+regexp.QuoteMeta(e.dir)+`$`,
		`(?m)^Dry run: nothing was changed\.`)
	e.noOut(out, `(?m)^Twinnel removed`)

	if after := e.tree(); !sameTree(before, after) {
		t.Errorf("a dry run changed the install:\nbefore %v\nafter  %v", before, after)
	}
	for _, h := range e.held[1:] {
		if !h.running() {
			t.Errorf("a dry run stopped %s", h.name)
		}
	}
}

// The uninstall the app's 完全移除 runs: the installed install.ps1
// -Uninstall, from Windows PowerShell or PowerShell 7.
func (e *installEnv) uninstall() {
	t := e.t
	out, code := e.run(filepath.Join(e.dir, "install.ps1"), "-Uninstall", "-InstallDir", e.dir)
	if code != 0 {
		t.Fatalf("uninstall: exit %d", code)
	}
	e.wantOut(out, `(?m)^stop twinnel\.exe`, `(?m)^remove `+regexp.QuoteMeta(e.dir)+`$`, `(?m)^Twinnel removed\.`)
	e.noOut(out, `(?i)exception|argument types`)
	if _, err := os.Stat(e.dir); !os.IsNotExist(err) {
		t.Errorf("%s still there: %v", e.dir, err)
	}
	if e.onPath() {
		t.Error("bin still on the user's PATH")
	}
	if _, err := os.Stat(shortcutPath()); !os.IsNotExist(err) {
		t.Errorf("Start menu shortcut still there: %v", err)
	}
	for _, h := range e.held {
		select {
		case <-h.done:
		case <-time.After(10 * time.Second):
			t.Errorf("%s still running", h.name)
		}
	}
	// Again: nothing to do, still fine.
	out, code = e.run(filepath.Join(e.src1, "install.ps1"), "-Uninstall", "-InstallDir", e.dir)
	if code != 0 {
		t.Fatalf("second uninstall: exit %d", code)
	}
	e.wantOut(out, `(?m)^Twinnel removed\.`)
}

// tree: each file under the install with its size and time.
func (e *installEnv) tree() map[string]string {
	m := map[string]string{}
	_ = filepath.Walk(e.dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			m[p] = fmt.Sprint(fi.ModTime().UnixNano(), " ", fi.Size())
		}
		return nil
	})
	return m
}

func sameTree(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
