//go:build !darwin && !linux && !windows

package main

import (
	"context"
	"errors"

	"github.com/l3ony2k/twinnel/core/engine"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
)

const secureSecrets = "" // --secrets auto when running as root

func newKeychain(string) (store.Secrets, error) {
	return nil, errors.New("--secrets keychain is only available on macOS")
}

// No service manager on this platform.
func platformServices() service.Manager { return nil }

func setupPlatform(e *engine.Engine, o *options) func(ctx context.Context) {
	return func(context.Context) {}
}
