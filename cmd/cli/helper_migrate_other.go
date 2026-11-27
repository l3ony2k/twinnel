//go:build !unix

package main

import "io/fs"

func lchownLike(string, fs.FileInfo) error { return nil }
