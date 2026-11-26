//go:build unix

package engine

import (
	"io/fs"
	"os"
	"syscall"
)

// keepOwner gives path the owner of the file it replaced (old), so a root
// helper rewriting an adopted config in a user's home does not leave it
// root-owned. Only root can chown; for others it is a no-op.
func keepOwner(path string, old fs.FileInfo) {
	if old == nil || os.Geteuid() != 0 {
		return
	}
	if st, ok := old.Sys().(*syscall.Stat_t); ok {
		_ = os.Lchown(path, int(st.Uid), int(st.Gid))
	}
}
