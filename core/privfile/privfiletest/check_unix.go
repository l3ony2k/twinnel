//go:build !windows

package privfiletest

import (
	"fmt"
	"os"
)

func check(path string, dir bool) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	want := os.FileMode(0o600)
	if dir {
		want = 0o700
	}
	if fi.Mode().Perm() != want {
		return fmt.Errorf("%s: mode %v, want %v", path, fi.Mode().Perm(), want)
	}
	return nil
}
