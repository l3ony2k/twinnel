//go:build unix

package main

import (
	"io/fs"
	"os"
	"syscall"
)

// lchownLike gives p the owner and group of fi.
func lchownLike(p string, fi fs.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if int(st.Uid) == os.Getuid() && int(st.Gid) == os.Getgid() {
		return nil
	}
	return os.Lchown(p, int(st.Uid), int(st.Gid))
}
