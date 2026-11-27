//go:build !darwin

package main

import (
	"context"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/status"
)

// newHealthSource: no Env for this platform yet.
func newHealthSource(status.Config) status.HealthSource { return nil }

// newEgressProbe: no egress test on this platform yet.
func newEgressProbe(status.Config) func(context.Context) *health.CheckReport { return nil }
