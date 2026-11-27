package svchost

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestResolveProgram(t *testing.T) {
	argv := []string{`C:\ProgramData\Twinnel\kernels\mihomo\current\mihomo.exe`, "-d", `C:\cfg`}
	got := ResolveProgram(argv, func(string) (string, error) {
		return `C:\ProgramData\Twinnel\kernels\mihomo\v1.2.3\mihomo.exe`, nil
	})
	if got[0] != `C:\ProgramData\Twinnel\kernels\mihomo\v1.2.3\mihomo.exe` || !slices.Equal(got[1:], argv[1:]) {
		t.Fatalf("%v", got)
	}
	if argv[0] != `C:\ProgramData\Twinnel\kernels\mihomo\current\mihomo.exe` {
		t.Fatal("argv changed in place")
	}
	if got := ResolveProgram(argv, func(string) (string, error) { return "", errors.New("gone") }); !slices.Equal(got, argv) {
		t.Fatalf("unresolvable path changed: %v", got)
	}
}

// With a real directory link, as the kernel manager makes "current".
func TestResolveProgramLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}
	dir := t.TempDir()
	ver := filepath.Join(dir, "v1.2.3")
	os.MkdirAll(ver, 0o755)
	os.WriteFile(filepath.Join(ver, "mihomo"), []byte("x"), 0o755)
	if err := os.Symlink("v1.2.3", filepath.Join(dir, "current")); err != nil {
		t.Fatal(err)
	}
	got := ResolveProgram([]string{filepath.Join(dir, "current", "mihomo")}, filepath.EvalSymlinks)
	want, _ := filepath.EvalSymlinks(filepath.Join(ver, "mihomo"))
	if got[0] != want {
		t.Fatalf("%s, want %s", got[0], want)
	}
}
