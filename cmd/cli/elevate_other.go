//go:build !darwin && !linux && !windows

package main

import (
	"errors"
	"os"
)

func platformEUID() int { return os.Geteuid() }

func elevate(op string, f *installFlags, exe string, rargs []string) error {
	return errors.New("elevation is not implemented on this platform")
}

func attachOutput(path string) error { return nil }
