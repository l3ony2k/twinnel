//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/l3ony2k/twinnel/core/sysdns"
	"github.com/l3ony2k/twinnel/platform/darwin"
)

// restoreSystemDNS writes the system DNS back from <state>/dns-backup.json
// (as root, during uninstall: the helper may already be gone).
var restoreSystemDNS = func(ctx context.Context, stateDir string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	m := &sysdns.Manager{P: darwin.SystemDNS{Run: darwin.ExecRunner(10 * time.Second)}, Path: filepath.Join(stateDir, "dns-backup.json")}
	st, err := m.Restore(ctx)
	if errors.Is(err, sysdns.ErrNoBackup) {
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "# system DNS written back on %d network service(s)\n", len(st.Changed))
	return nil
}
