//go:build windows

package kernel

import (
	"errors"
	"io/fs"
	"os"
)

// replaceLink: Windows cannot rename a directory symlink over an existing
// one, so the old link goes first (a microsecond gap; mihomo is restarted
// after a switch anyway, and the running mihomo.exe lives in its version
// dir, which is not touched).
func replaceLink(tmp, dst string) error {
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&fs.ModeSymlink == 0 {
			return errors.New(dst + " exists and is not a link")
		}
		if err := os.Remove(dst); err != nil {
			return err
		}
	}
	return os.Rename(tmp, dst)
}
