//go:build !windows

package main

import "github.com/l3ony2k/twinnel/core/status"

// platformTrayIcon: nil, the menu bar mark (icons.go) everywhere but
// Windows.
func platformTrayIcon(status.TrayIcon) []byte { return nil }
