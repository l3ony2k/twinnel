//go:build !darwin && !linux

package main

import (
	"fmt"
	"runtime"

	"github.com/l3ony2k/duet/core/health"
)

const configDiscovery = "none on this platform"

func newHealthEnv(healthEnvOptions) (health.Env, error) {
	return nil, fmt.Errorf("duet doctor is not available on %s yet", runtime.GOOS)
}
