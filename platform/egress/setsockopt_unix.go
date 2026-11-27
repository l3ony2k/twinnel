//go:build darwin || linux

package egress

import "syscall"

func setsockopt(fd uintptr, o SockOpt) error {
	if o.IsStr {
		return syscall.SetsockoptString(int(fd), o.Level, o.Name, o.Str)
	}
	return syscall.SetsockoptInt(int(fd), o.Level, o.Name, o.Int)
}
