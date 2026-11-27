//go:build !windows

package windows

import (
	"context"

	"github.com/l3ony2k/twinnel/core/foreign"
)

// PortOwner finds the process listening on a port; iphlpapi exists only
// on Windows, so elsewhere nothing is found.
func PortOwner(context.Context, int) (*foreign.Process, error) { return nil, nil }
