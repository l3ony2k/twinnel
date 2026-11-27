//go:build windows

package health

import (
	"errors"
	"syscall"
)

// wsaeConnRefused is WSAECONNREFUSED: Winsock's errno is not
// syscall.ECONNREFUSED.
const wsaeConnRefused = syscall.Errno(10061)

func isRefused(err error) bool {
	return errors.Is(err, wsaeConnRefused) || errors.Is(err, syscall.ECONNREFUSED)
}
