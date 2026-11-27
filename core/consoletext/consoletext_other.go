//go:build !windows

package consoletext

// fromOEM: only Windows' console programs write in an OEM code page.
func fromOEM([]byte) ([]byte, bool) { return nil, false }
