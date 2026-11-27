//go:build !windows

package rpc

import "os"

// DefaultSocket is where the installed helper listens on macOS and Linux.
const DefaultSocket = "/var/run/twinnel/helper.sock"

// DefaultTransport is the platform's transport: a unix socket.
func DefaultTransport() Transport { return NewUnixTransport() }

// Self is this process's identity in Peer form (logs, status): its uid,
// login name and pid.
func Self() Peer {
	uid := os.Getuid()
	return Peer{UID: uid, PID: os.Getpid(), User: userName(uid)}
}
