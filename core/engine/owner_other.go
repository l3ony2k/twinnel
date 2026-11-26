//go:build !unix

package engine

import "io/fs"

func statOwner(fs.FileInfo) (int, int, bool) { return 0, 0, false }
