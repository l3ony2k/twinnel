//go:build !darwin

package main

import "github.com/l3ony2k/duet/core/service"

// Linux (systemd) and Windows service managers come later.
func platformServices() service.Manager { return nil }
