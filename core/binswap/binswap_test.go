package binswap

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// windowsLocks fakes Windows' rules for running images: a running file
// can be renamed (it stays running under its new name) but not deleted,
// and nothing can be renamed over it.
type windowsLocks struct {
	running map[string]bool
	failOn  string // renaming this file fails (a fault)
}

func (w *windowsLocks) install(t *testing.T) {
	t.Helper()
	saved := ops
	t.Cleanup(func() { ops = saved })
	ops.rename = func(old, new string) error {
		if w.running[new] || old == w.failOn {
			return &os.LinkError{Op: "rename", Old: old, New: new, Err: errors.New("Access is denied.")}
		}
		if err := os.Rename(old, new); err != nil {
			return err
		}
		if w.running[old] {
			delete(w.running, old)
			w.running[new] = true
		}
		return nil
	}
	ops.remove = func(p string) error {
		if w.running[p] {
			return &os.PathError{Op: "remove", Path: p, Err: errors.New("Access is denied.")}
		}
		return os.Remove(p)
	}
	ops.now = func() time.Time { return time.Unix(0, 1764300000000000000) }
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

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func TestSwapInUse(t *testing.T) {
	src, bin := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "twinnel.exe"), "new cli")
	write(t, filepath.Join(src, "twinnel-helper.exe"), "new helper")
	cli, helper := filepath.Join(bin, "twinnel.exe"), filepath.Join(bin, "twinnel-helper.exe")
	write(t, cli, "old cli")
	write(t, helper, "old helper")
	w := &windowsLocks{running: map[string]bool{cli: true}} // an agent's twinnel mcp
	w.install(t)

	err := Swap([]File{
		{Src: filepath.Join(src, "twinnel.exe"), Dst: cli, Mode: 0o755},
		{Src: filepath.Join(src, "twinnel-helper.exe"), Dst: helper, Mode: 0o755},
	})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, cli) != "new cli" || read(t, helper) != "new helper" {
		t.Fatal("not replaced")
	}
	// The running one stays aside; the idle one is gone.
	old := cli + OldInfix + "1764300000000000000"
	want := []string{"twinnel-helper.exe", "twinnel.exe", "twinnel.exe.old-1764300000000000000"}
	if got := names(t, bin); !slices.Equal(got, want) {
		t.Fatalf("files %v, want %v", got, want)
	}
	if !w.running[old] || read(t, old) != "old cli" {
		t.Fatal("the running image should keep running, moved aside")
	}

	// Still running at the next swap: stays. Exited: cleaned.
	if got := Clean(cli); !slices.Equal(got, []string{old}) {
		t.Fatalf("in use %v", got)
	}
	delete(w.running, old)
	if got := Clean(cli); len(got) != 0 {
		t.Fatalf("in use %v", got)
	}
	if got := names(t, bin); !slices.Equal(got, []string{"twinnel-helper.exe", "twinnel.exe"}) {
		t.Fatalf("after clean %v", got)
	}
}

// A failure half way puts back the files already swapped: all old.
func TestSwapAllOrNone(t *testing.T) {
	src, dir := t.TempDir(), t.TempDir()
	a, b := filepath.Join(dir, "Twinnel.exe"), filepath.Join(dir, "bin", "twinnel.exe")
	write(t, filepath.Join(src, "a"), "new a")
	write(t, filepath.Join(src, "b"), "new b")
	write(t, a, "old a")
	write(t, b, "old b")
	w := &windowsLocks{running: map[string]bool{a: true}, failOn: b + NewInfix + "1764300000000000000"}
	w.install(t)

	err := Swap([]File{
		{Src: filepath.Join(src, "a"), Dst: a, Mode: 0o755},
		{Src: filepath.Join(src, "b"), Dst: b, Mode: 0o755},
	})
	if err == nil {
		t.Fatal("want an error")
	}
	if read(t, a) != "old a" || read(t, b) != "old b" {
		t.Fatalf("not rolled back: %q %q", read(t, a), read(t, b))
	}
	if !w.running[a] {
		t.Fatal("the running image should be back under its name")
	}
	if got := names(t, dir); !slices.Equal(got, []string{"Twinnel.exe", "bin"}) {
		t.Fatalf("leftovers %v", got)
	}
	if got := names(t, filepath.Join(dir, "bin")); !slices.Equal(got, []string{"twinnel.exe"}) {
		t.Fatalf("leftovers %v", got)
	}
}

// A missing source stops before anything is touched.
func TestSwapStageFails(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "twinnel.exe")
	write(t, dst, "old")
	(&windowsLocks{running: map[string]bool{}}).install(t)
	if err := Swap([]File{{Src: filepath.Join(dir, "missing"), Dst: dst, Mode: 0o755}}); err == nil {
		t.Fatal("want an error")
	}
	if read(t, dst) != "old" || len(names(t, dir)) != 1 {
		t.Fatalf("touched: %v", names(t, dir))
	}
}

func TestReplaceNewFileAndMode(t *testing.T) {
	src, dir := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "x"), "x")
	dst := filepath.Join(dir, "sub", "twinnel-svchost.exe")
	if err := Replace(filepath.Join(src, "x"), dst, 0o755); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dst)
	if err != nil || fi.Mode().Perm() != 0o755 && os.PathSeparator == '/' {
		t.Fatalf("%v %v", fi, err)
	}
}

func TestLeftovers(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{
		"twinnel.exe", "twinnel.exe.old-1", "twinnel.exe.new-22", "twinnel.exe.old-x", "twinnel.exe.old-",
		"twinnel-helper.exe.old-3", "twinnel.exe.bak",
	} {
		write(t, filepath.Join(dir, n), n)
	}
	var got []string
	for _, p := range Leftovers(filepath.Join(dir, "twinnel.exe")) {
		got = append(got, filepath.Base(p))
	}
	if !slices.Equal(got, []string{"twinnel.exe.new-22", "twinnel.exe.old-1"}) {
		t.Fatalf("%v", got)
	}
}
