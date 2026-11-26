//go:build !unix

package store

import "io/fs"

func ownerUID(fs.FileInfo) (int, bool) { return 0, false }
