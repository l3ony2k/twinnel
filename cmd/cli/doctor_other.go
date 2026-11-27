//go:build !darwin && !linux

package main

import (
	"fmt"
	"runtime"

	"github.com/l3ony2k/twinnel/core/health"
)

const configDiscovery = "none on this platform"

func newHealthEnv(healthEnvOptions) (health.Env, error) {
	return nil, fmt.Errorf("twinnel doctor is not available on %s yet", runtime.GOOS)
}
