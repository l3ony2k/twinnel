//go:build windows

package rpc

// DefaultSocket is the named pipe the installed helper listens on.
const DefaultSocket = `\\.\pipe\twinnel-helper`

// DefaultTransport is the platform's transport: a named pipe.
func DefaultTransport() Transport { return NewPipeTransport() }
