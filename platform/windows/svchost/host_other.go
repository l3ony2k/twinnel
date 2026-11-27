//go:build !windows

package svchost

import (
	"fmt"
	"os"
)

// Main is the service host's entry point; it only works on Windows.
func Main(args []string) int {
	fmt.Fprintln(os.Stderr, "twinnel-helper svchost: only on Windows")
	return 2
}
