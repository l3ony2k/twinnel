//go:build windows

package i18n

// Windows words its platform texts its own way (platform.go).
func init() { applyDesktop("windows") }
