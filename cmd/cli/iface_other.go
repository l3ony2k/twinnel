//go:build !darwin && !linux && !windows

package main

import "errors"

func tailscaleIface() (string, error) {
	return "", errors.New("interface detection is not implemented on this platform")
}
