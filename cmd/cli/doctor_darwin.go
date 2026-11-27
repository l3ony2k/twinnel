//go:build darwin

package main

import (
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/platform/darwin"
)

const configDiscovery = "discovered from the mihomo LaunchDaemon"

func newHealthEnv(o healthEnvOptions) (health.Env, error) {
	return darwin.New(darwin.Options{Controller: o.Controller, Secret: o.Secret, ConfigPath: o.ConfigPath, Settings: o.Settings, Dataplane: health.NetProbes{}}), nil
}
