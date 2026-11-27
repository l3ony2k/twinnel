package privfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSDDLFor(t *testing.T) {
	const user = "S-1-5-21-1-2-3-1001"
	for _, c := range []struct {
		sid  string
		dir  bool
		want string
	}{
		{user, false, "O:" + user + "D:P(A;;FA;;;" + user + ")(A;;FA;;;SY)(A;;FA;;;BA)"},
		{user, true, "O:" + user + "D:P(A;OICI;FA;;;" + user + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"},
		{"S-1-5-18", false, "O:S-1-5-18D:P(A;;FA;;;S-1-5-18)(A;;FA;;;BA)"},
	} {
		if got := sddlFor(c.sid, c.dir); got != c.want {
			t.Errorf("sddlFor(%s, %v) = %s, want %s", c.sid, c.dir, got, c.want)
		}
	}
}

func TestWriteFileReplacesAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "secret.zip")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(p, []byte("synthetic-secret")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "synthetic-secret" {
		t.Fatalf("content %q", b)
	}
	assertPrivate(t, p, false)
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("left behind: %v", ents)
	}
}

func TestWriteFileErrorLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFile(filepath.Join(dir, "missing", "x"), []byte("x")); err == nil {
		t.Fatal("wrote into a missing dir")
	}
	// A dir at the target: the rename fails, the temp file must go.
	target := filepath.Join(dir, "taken")
	os.Mkdir(target, 0o700)
	os.WriteFile(filepath.Join(target, "f"), nil, 0o600)
	if err := WriteFile(target, []byte("x")); err == nil {
		t.Fatal("replaced a non-empty dir")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("left behind: %v", ents)
	}
}

func TestCreateIsExclusive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	f, err := Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("x"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	assertPrivate(t, p, false)
	if _, err := Create(p); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second Create: %v, want ErrExist", err)
	}
}

func TestDirs(t *testing.T) {
	base := t.TempDir()
	d, err := MkdirTemp(base, "twinnel-test-")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(d) != base || len(filepath.Base(d)) <= len("twinnel-test-") {
		t.Fatalf("MkdirTemp = %s", d)
	}
	assertPrivate(t, d, true)
	deep := filepath.Join(base, "a", "b")
	if err := MkdirAll(deep); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(deep); err != nil {
		t.Fatalf("again: %v", err)
	}
	assertPrivate(t, deep, true)
	assertPrivate(t, filepath.Dir(deep), true)
	if runtime.GOOS == "windows" {
		// A plain file made inside a private dir inherits its ACEs.
		f := filepath.Join(deep, "inherits")
		os.WriteFile(f, []byte("x"), 0o644)
		assertInherited(t, f)
	}
}
