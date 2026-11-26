//go:build !darwin && !linux

package rpc

import (
	"errors"
	"net"
)

func peerCred(*net.UnixConn) (Peer, error) {
	return Peer{}, errors.New("peer credentials are not supported on this platform")
}
