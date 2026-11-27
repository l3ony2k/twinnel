//go:build live

package mihomo

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLive runs read-only requests against a real controller:
//
//	TWINNEL_CONTROLLER=127.0.0.1:9090 TWINNEL_MIHOMO_SECRET=... go test -tags live ./core/mihomo
func TestLive(t *testing.T) {
	c, err := New(Options{Address: os.Getenv("TWINNEL_CONTROLLER"), Secret: os.Getenv("TWINNEL_MIHOMO_SECRET")})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	v, err := c.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := c.Configs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("version %s mode %s tun %v %s", v.Version, cfg.Mode, cfg.Tun.Enable, cfg.Tun.Device)
	all, err := c.Proxies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range Groups(all) {
		t.Logf("group %s (%s) now=%s members=%d", g.Name, g.Type, g.Now, len(g.All))
	}
	sctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	n := 0
	_ = c.StreamTraffic(sctx, func(Traffic) { n++ })
	if n == 0 {
		t.Error("no traffic samples")
	}
}
