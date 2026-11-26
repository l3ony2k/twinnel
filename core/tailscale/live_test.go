//go:build live

package tailscale

import (
	"context"
	"os"
	"runtime"
	"testing"
)

// TestLive reads the local node through the LocalAPI (read-only):
//
//	go test -tags live ./core/tailscale
func TestLive(t *testing.T) {
	s, err := New(LocalAPI(nil), false).State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("state %s version %s self %s peers %d online %d exit %v",
		s.BackendState, s.Version, s.Self.Name, len(s.Peers), s.OnlinePeers(), s.ExitNode != nil)
	for _, p := range s.Peers {
		t.Logf("  %-16s %-8s online=%v path=%s relay=%s", p.Name, p.OS, p.Online, p.Path(), p.Relay)
	}
	in := DetectInstall(runtime.GOOS, func(p string) bool { _, err := os.Stat(p); return err == nil })
	t.Logf("install %+v", in)
}
