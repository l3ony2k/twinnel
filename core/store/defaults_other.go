//go:build !windows

package store

// DefaultDir is the state dir of the installed helper on macOS (Linux maps
// it to /var/lib/twinnel, platform/linux.InstallPaths).
const DefaultDir = "/Library/Application Support/twinnel"

// Default service labels: launchd labels (Linux rewrites them to its unit
// names, platform/linux.LinuxLabels).
const (
	DefaultMihomoLabel    = "com.l3ony2k.twinnel.mihomo"
	DefaultTailscaleLabel = "com.tailscale.tailscaled"
	DefaultHelperLabel    = "com.l3ony2k.twinnel.helper"
)
