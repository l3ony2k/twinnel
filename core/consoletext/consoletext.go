// Package consoletext turns what console programs print into UTF-8 before
// Twinnel keeps it (errors, history, events, logs, JSON files).
//
// Windows' own tools (netsh, sc, schtasks, reg, msiexec) print in the OEM
// code page of the system locale (936/GBK on a Chinese system), not UTF-8.
// Kept as is, a Chinese error message becomes invalid UTF-8 and every JSON
// encoder turns it into U+FFFD. Decode keeps output that is already valid
// UTF-8 (Go programs such as tailscale and mihomo, and plain ASCII) and
// converts the rest from the OEM code page. Elsewhere it changes nothing.
package consoletext

import "unicode/utf8"

// Decode returns b as UTF-8 (see the package comment).
func Decode(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	if d, ok := fromOEM(b); ok {
		return d
	}
	return b
}
