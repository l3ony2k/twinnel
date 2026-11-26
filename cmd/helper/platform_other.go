//go:build !darwin && !linux

package main

import (
	"context"
	"errors"

	"github.com/l3ony2k/duet/core/engine"
	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/core/store"
)

const hasKeychain = false

func newKeychain(string) (store.Secrets, error) {
	return nil, errors.New("--secrets keychain is only available on macOS")
}

// Linux (systemd) and Windows service managers come later.
func platformServices() service.Manager { return nil }

func setupPlatform(e *engine.Engine, o *options) func(ctx context.Context) {
	return func(context.Context) {}
}
