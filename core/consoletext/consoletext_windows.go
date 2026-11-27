//go:build windows

package consoletext

import (
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

var procGetOEMCP = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetOEMCP")

// fromOEM converts b from the OEM code page (what console programs write
// to a pipe) with MultiByteToWideChar.
func fromOEM(b []byte) ([]byte, bool) {
	if len(b) == 0 || len(b) > 1<<30 {
		return nil, false
	}
	cp, _, _ := procGetOEMCP.Call()
	if cp == 0 || cp == 65001 { // CP_UTF8: invalid UTF-8 stays invalid
		return nil, false
	}
	n, err := windows.MultiByteToWideChar(uint32(cp), 0, &b[0], int32(len(b)), nil, 0)
	if err != nil || n <= 0 {
		return nil, false
	}
	w := make([]uint16, n)
	if n, err = windows.MultiByteToWideChar(uint32(cp), 0, &b[0], int32(len(b)), &w[0], n); err != nil || n <= 0 {
		return nil, false
	}
	out := make([]byte, 0, len(b)*3/2)
	for _, r := range utf16.Decode(w[:n]) {
		out = utf8.AppendRune(out, r)
	}
	return out, true
}
