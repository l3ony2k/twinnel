package linux

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// testEnv is New for tests: every source of the machine's state a test
// leaves unset is a fake, never this host's. Commands come from
// fakeRunner, no program is on PATH, resolv.conf and resolved's upstream
// list do not exist, no unit dir is searched, the controller and mihomo's
// DNS listener are ports nothing listens on, and the default reader
// refuses any file outside the test's own (testdata, temp dirs). A test
// sets a field to give that source a fixture.
func testEnv(t *testing.T, o Options) *Env {
	t.Helper()
	if o.Run == nil {
		o.Run = fakeRunner(t)
	}
	if o.LookPath == nil {
		o.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if o.ResolvConf == "" {
		o.ResolvConf = missing
	}
	if o.UpstreamResolvConf == "" {
		o.UpstreamResolvConf = missing
	}
	if o.UnitDirs == nil {
		o.UnitDirs = []string{}
	}
	if o.Controller == "" {
		o.Controller = "127.0.0.1:1"
	}
	if o.DNSServer == "" {
		o.DNSServer = "127.0.0.1:1"
	}
	if o.ReadFile == nil {
		o.ReadFile = func(p string) ([]byte, error) {
			if !testOwned(p) {
				return nil, fmt.Errorf("test read the host's %s: %w", p, fs.ErrNotExist)
			}
			return os.ReadFile(p)
		}
	}
	return New(o)
}

// testOwned: a relative path (testdata) or one under the temp dir.
func testOwned(p string) bool {
	if !filepath.IsAbs(p) {
		return true
	}
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		tmp = os.TempDir()
	}
	r, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		r = filepath.Dir(p)
	}
	for _, root := range []string{os.TempDir(), tmp} {
		if rel, err := filepath.Rel(root, r); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func TestTestEnvReadsNothingOfTheHost(t *testing.T) {
	e := testEnv(t, Options{})
	if _, err := e.opt.ReadFile("/etc/resolv.conf"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read the host's resolv.conf: %v", err)
	}
	if _, err := e.Resolvers(context.Background()); err == nil {
		t.Fatal("resolvers came from somewhere")
	}
	p := filepath.Join(t.TempDir(), "x")
	os.WriteFile(p, []byte("x"), 0o600)
	if _, err := e.opt.ReadFile(p); err != nil {
		t.Fatalf("temp file: %v", err)
	}
}

// Tests build their Env with testEnv, so none silently reads this host
// (an Options field left empty means the real file, unit dirs, PATH).
// A test that needs New's real defaults says so on the line.
func TestTestsUseTestEnv(t *testing.T) {
	files, _ := filepath.Glob("*_test.go")
	re := regexp.MustCompile(`\bNew\(Options\{`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if re.MatchString(line) && !strings.Contains(line, "// New's defaults on purpose") && !strings.Contains(line, "return New(o)") {
				t.Errorf("%s:%d: build the Env with testEnv(t, Options{…}), not New: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
