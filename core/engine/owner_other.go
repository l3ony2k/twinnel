//go:build !unix

package engine

import "io/fs"

func keepOwner(string, fs.FileInfo) {}
