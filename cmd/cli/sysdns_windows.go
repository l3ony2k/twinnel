//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/l3ony2k/twinnel/core/sysdns"
	"github.com/l3ony2k/twinnel/platform/windows"
)

// restoreSystemDNS writes the adapters' DNS back from
// <state>\dns-backup.json (elevated, during uninstall: the helper may
// already be gone).
var restoreSystemDNS = func(ctx context.Context, stateDir string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	p := windows.SystemDNS{Sys: windows.NewSys(), Static: windows.NewStaticDNS(), Run: windows.ExecRunner(20 * time.Second)}
	m := &sysdns.Manager{P: p, Path: filepath.Join(stateDir, "dns-backup.json")}
	st, err := m.Restore(ctx)
	if errors.Is(err, sysdns.ErrNoBackup) {
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "# system DNS written back on %d network adapter(s)\n", len(st.Changed))
	return nil
}
