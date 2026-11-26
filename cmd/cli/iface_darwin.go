//go:build darwin

package main

import (
	"context"
	"time"

	"github.com/l3ony2k/duet/platform/darwin"
)

// tailscaleIface returns the interface that carries 100.100.100.100
// (read-only `route -n get`).
func tailscaleIface() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := darwin.New(darwin.Options{}).RouteGet(ctx, "100.100.100.100")
	return r.Interface, err
}
