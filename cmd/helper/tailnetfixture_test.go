package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/l3ony2k/twinnel/core/kernel"
)

// A helper on a status fixture answers tailscale.info from it: the
// fixture's version, no binary path and no latest version (nothing of the
// machine's Tailscale, nothing fetched).
func TestFixtureTailscaleInfo(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "status.json")
	if err := os.WriteFile(file, []byte(`{"Version":"1.90.6-t1234abcd-g5678","BackendState":"Running"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for goos, kind := range map[string]string{"windows": kernel.TSWindows, "darwin": kernel.TSBrew, "linux": kernel.TSBrew} {
		got := fileTailnet(file).Info(context.Background(), goos)
		if !got.Installed || got.Version != "1.90.6" || got.InstallKind != kind ||
			got.Path != "" || got.Latest != "" || got.UpdateAvailable || got.Error != "" {
			t.Errorf("%s: %+v", goos, got)
		}
	}
	if got := fileTailnet(filepath.Join(dir, "missing.json")).Info(context.Background(), "windows"); got.Installed || got.Error == "" {
		t.Errorf("missing fixture: %+v", got)
	}
}
