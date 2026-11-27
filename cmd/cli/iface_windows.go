//go:build windows

package main

import (
	"context"
	"time"

	"github.com/l3ony2k/twinnel/platform/windows"
)

// tailscaleIface returns the adapter that carries 100.100.100.100 (read
// from the route table; "Tailscale" normally).
func tailscaleIface() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return windows.New(windows.Options{}).TailscaleInterface(ctx)
}
