package main

import (
	"context"

	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/platform/windows"
)

// procs lists the processes, keeping CPU times between reads.
var procs windows.ProcessLister

// systemProcesses is the process list from the system itself: Windows
// has no ps. ok is true where this is the way to list them.
func systemProcesses(ctx context.Context) ([]status.ProcessEntry, bool, error) {
	list, err := procs.List(ctx)
	return list, true, err
}
