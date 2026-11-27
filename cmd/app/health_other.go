//go:build !darwin

package main

import (
	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/status"
)

// healthEnv: no Env for this platform yet.
func (realProbes) healthEnv(status.Config, health.Settings) health.Env { return nil }
