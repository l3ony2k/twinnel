//go:build linux

package i18n

// Linux words its few platform texts its own way (desktop.go).
func init() { applyDesktop("linux") }
