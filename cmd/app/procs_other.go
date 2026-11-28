//go:build !windows && !linux

package main

import (
	"context"

	"github.com/l3ony2k/twinnel/core/status"
)

// systemProcesses: ps lists the processes on macOS (realProbes.processes).
func systemProcesses(context.Context) ([]status.ProcessEntry, bool, error) {
	return nil, false, nil
}
