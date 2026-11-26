//go:build !darwin

package main

import (
	"context"

	"github.com/l3ony2k/duet/core/engine"
	"github.com/l3ony2k/duet/core/service"
)

// Linux (systemd) and Windows service managers come later.
func platformServices() service.Manager { return nil }

func setupPlatform(e *engine.Engine, o *options) func(ctx context.Context) {
	return func(context.Context) {}
}
