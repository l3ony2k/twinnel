package binsane

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	pe := append([]byte("MZ\x90\x00"), make([]byte, 100)...)
	elf := append([]byte("\x7fELF"), make([]byte, 100)...)
	macho := append([]byte{0xcf, 0xfa, 0xed, 0xfe}, make([]byte, 100)...)
	for _, tt := range []struct {
		name string
		path string
		goos string
		want Problem
	}{
		{"pe", write("a.exe", pe), "windows", OK},
		{"zeroed", write("z.exe", make([]byte, 17<<10)), "windows", Zeroed},
		{"empty", write("e.exe", nil), "windows", Empty},
		{"elf on windows", write("b.exe", elf), "windows", BadHeader},
		{"elf", write("c", elf), "linux", OK},
		{"macho", write("d", macho), "darwin", OK},
		{"pe on darwin", write("f", pe), "darwin", BadHeader},
		{"missing", filepath.Join(dir, "nope"), "linux", Missing},
	} {
		if got := Check(tt.path, tt.goos, false); got.Problem != tt.want {
			t.Errorf("%s: %+v, want %q", tt.name, got, tt.want)
		}
	}
	// The signature hook runs only when asked.
	old := VerifySignature
	t.Cleanup(func() { VerifySignature = old })
	VerifySignature = func(string) error { return errors.New("TRUST_E_BAD_DIGEST") }
	p := write("s.exe", pe)
	if r := Check(p, "windows", false); r.Problem != OK {
		t.Fatal(r)
	}
	if r := Check(p, "windows", true); r.Problem != BadSign || !r.Problem.Damaged() {
		t.Fatal(r)
	}
}

func TestDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	p := filepath.Join(t.TempDir(), "locked")
	if err := os.WriteFile(p, []byte("\x7fELF...."), 0o000); err != nil {
		t.Fatal(err)
	}
	if r := Check(p, "linux", false); r.Problem != Denied || r.Problem.Damaged() {
		t.Fatal(r)
	}
}
