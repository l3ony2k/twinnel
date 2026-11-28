package main

import (
	"context"

	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/platform/linux"
)

// systemProcesses reads /proc: procps' ps takes no BSD-style -axo
// reliably and minimal systems have no ps at all.
func systemProcesses(ctx context.Context) ([]status.ProcessEntry, bool, error) {
	list, err := linux.Processes(ctx, "/proc")
	return list, true, err
}
