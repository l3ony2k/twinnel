//go:build !darwin && !linux && !windows

package egress

import (
	"fmt"
	"runtime"

	"github.com/l3ony2k/twinnel/core/health"
)

func setsockopt(uintptr, SockOpt) error {
	return fmt.Errorf("bind to interface on %s: %w", runtime.GOOS, health.ErrNotSupported)
}
