//go:build darwin

package rpc

import (
	"net"

	"golang.org/x/sys/unix"
)

func peerCred(c *net.UnixConn) (Peer, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return Peer{}, err
	}
	var (
		p    Peer
		cerr error
	)
	err = raw.Control(func(fd uintptr) {
		var x *unix.Xucred
		x, cerr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if cerr != nil {
			return
		}
		p.UID = int(x.Uid)
		n := int(x.Ngroups)
		if n > len(x.Groups) {
			n = len(x.Groups)
		}
		for _, g := range x.Groups[:n] {
			p.GIDs = append(p.GIDs, int(g))
		}
		if pid, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID); err == nil {
			p.PID = pid
		}
	})
	if err != nil {
		return Peer{}, err
	}
	return p, cerr
}
