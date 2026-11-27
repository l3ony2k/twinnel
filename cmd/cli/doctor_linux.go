//go:build linux

package main

import (
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/platform/linux"
)

const configDiscovery = "discovered from a systemd unit running mihomo, else /etc/mihomo/config.yaml, else ~/.config/mihomo/config.yaml"

func newHealthEnv(o healthEnvOptions) (health.Env, error) {
	return linux.New(linux.Options{Controller: o.Controller, ConfigPath: o.ConfigPath, Settings: o.Settings}), nil
}
