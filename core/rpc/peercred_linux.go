//go:build linux

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
		var u *unix.Ucred
		u, cerr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if cerr != nil {
			return
		}
		p = Peer{UID: int(u.Uid), GIDs: []int{int(u.Gid)}, PID: int(u.Pid)}
	})
	if err != nil {
		return Peer{}, err
	}
	return p, cerr
}
