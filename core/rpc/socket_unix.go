//go:build !windows

package rpc

// DefaultSocket is where the installed helper listens on macOS and Linux.
const DefaultSocket = "/var/run/twinnel/helper.sock"

// DefaultTransport is the platform's transport: a unix socket.
func DefaultTransport() Transport { return NewUnixTransport() }
