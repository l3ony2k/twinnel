//go:build linux

package main

import (
	"context"
	"time"

	"github.com/l3ony2k/twinnel/platform/linux"
)

// tailscaleIface returns the device that carries 100.100.100.100
// (read-only `ip route get`; tailscale0 normally).
func tailscaleIface() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return linux.NewProbe().TailscaleInterface(ctx)
}
