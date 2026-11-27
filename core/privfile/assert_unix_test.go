//go:build !windows

package privfile

import (
	"os"
	"testing"
)

func assertPrivate(t *testing.T, path string, dir bool) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	want := os.FileMode(0o600)
	if dir {
		want = 0o700
	}
	if fi.Mode().Perm() != want {
		t.Errorf("%s: mode %v, want %v", path, fi.Mode().Perm(), want)
	}
}

func assertInherited(*testing.T, string) {}
