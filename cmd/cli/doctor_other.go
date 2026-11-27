//go:build !darwin && !linux && !windows

package main

import (
	"fmt"
	"runtime"

	"github.com/l3ony2k/twinnel/core/health"
)

const configDiscovery = "none on this platform"

func platformHealthEnv(healthEnvOptions) (health.Env, error) {
	return nil, fmt.Errorf("twinnel doctor is not available on %s yet", runtime.GOOS)
}
