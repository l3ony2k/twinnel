//go:build !unix

package takeover

import "os"

func sameOwnerOrRoot(a, b os.FileInfo) bool { return true }
