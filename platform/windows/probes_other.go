//go:build !windows

package windows

import (
	"context"

	"github.com/l3ony2k/twinnel/core/health"
)

func icmpEcho(context.Context, string) error { return health.ErrNotSupported }
