//go:build !windows

package kernel

import "os"

// replaceLink renames the new symlink over the old one (atomic).
func replaceLink(tmp, dst string) error { return os.Rename(tmp, dst) }
