//go:build !darwin

package main

import "github.com/l3ony2k/duet/core/status"

// newHealthSource: no Env for this platform yet.
func newHealthSource(status.Config) status.HealthSource { return nil }
