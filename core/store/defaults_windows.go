//go:build windows

package store

import (
	"os"
	"path/filepath"
)

// DefaultDir is %ProgramData%\Twinnel.
var DefaultDir = filepath.Join(programData(), "Twinnel")

func programData() string {
	if d := os.Getenv("ProgramData"); filepath.IsAbs(d) {
		return d
	}
	return `C:\ProgramData`
}

// Default service names: the helper's and mihomo's Windows services, and
// the official Tailscale client's.
const (
	DefaultMihomoLabel    = "TwinnelMihomo"
	DefaultTailscaleLabel = "Tailscale"
	DefaultHelperLabel    = "TwinnelHelper"
)
