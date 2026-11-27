//go:build windows

package main

import (
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/platform/windows"
)

const configDiscovery = `%ProgramData%\Twinnel\mihomo\config.yaml, else %USERPROFILE%\.config\mihomo\config.yaml`

func platformHealthEnv(o healthEnvOptions) (health.Env, error) {
	return windows.New(windows.Options{Controller: o.Controller, Secret: o.Secret, ConfigPath: o.ConfigPath, Settings: o.Settings, Dataplane: windows.Probes()}), nil
}
