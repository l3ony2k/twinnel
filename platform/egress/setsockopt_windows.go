//go:build windows

package egress

import (
	"errors"
	"syscall"
)

func setsockopt(fd uintptr, o SockOpt) error {
	if o.IsStr {
		return errors.New("string socket options are not used on Windows")
	}
	// SetsockoptInt passes the value as a 4-byte int32: for IP_UNICAST_IF
	// BindOption already put the index in network byte order.
	return syscall.SetsockoptInt(syscall.Handle(fd), o.Level, o.Name, o.Int)
}
