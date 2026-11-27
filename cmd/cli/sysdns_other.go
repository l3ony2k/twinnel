//go:build !darwin

package main

import (
	"context"
	"os"
	"path/filepath"
)

// restoreSystemDNS: Twinnel sets the system DNS only on macOS so far; a
// backup elsewhere is left for a later version and reported.
var restoreSystemDNS = func(ctx context.Context, stateDir string) error {
	if _, err := os.Stat(filepath.Join(stateDir, "dns-backup.json")); err == nil {
		_, _ = stdout.Write([]byte("# dns-backup.json found but restoring the system DNS is not supported on this platform\n"))
	}
	return nil
}
