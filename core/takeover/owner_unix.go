//go:build unix

package takeover

import (
	"os"
	"syscall"
)

// sameOwnerOrRoot: a is owned by root or by b's owner.
func sameOwnerOrRoot(a, b os.FileInfo) bool {
	as, ok1 := a.Sys().(*syscall.Stat_t)
	bs, ok2 := b.Sys().(*syscall.Stat_t)
	if !ok1 || !ok2 {
		return true
	}
	return as.Uid == 0 || as.Uid == bs.Uid
}
