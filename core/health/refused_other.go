//go:build !windows

package health

import (
	"errors"
	"syscall"
)

func isRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
