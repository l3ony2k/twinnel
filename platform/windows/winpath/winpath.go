// Package winpath names where Twinnel and the official Tailscale client live
// on Windows. No build tag: the values are plain strings, built from the
// environment variables Windows sets (%ProgramFiles%, %ProgramData%), with
// the stock locations when they are missing (a test on another OS).
package winpath

import (
	"os"
	"strings"
)

// Join joins Windows path elements with backslashes (filepath.Join would
// use slashes when this runs on macOS or Linux, e.g. in tests).
func Join(elem ...string) string {
	var parts []string
	for i, e := range elem {
		e = strings.ReplaceAll(e, "/", `\`)
		if i > 0 {
			e = strings.Trim(e, `\`)
		} else {
			e = strings.TrimRight(e, `\`)
		}
		if e != "" {
			parts = append(parts, e)
		}
	}
	return strings.Join(parts, `\`)
}

// Dir is the directory part of a Windows path ("" for a bare name).
func Dir(p string) string {
	i := strings.LastIndex(p, `\`)
	if i < 0 {
		return ""
	}
	return p[:i]
}

func env(name, def string) string {
	if v := os.Getenv(name); len(v) >= 3 && v[1] == ':' && v[2] == '\\' {
		return v
	}
	return def
}

// ProgramFiles is %ProgramFiles% (64-bit programs).
func ProgramFiles() string { return env("ProgramFiles", `C:\Program Files`) }

// ProgramData is %ProgramData%.
func ProgramData() string { return env("ProgramData", `C:\ProgramData`) }

// InstallDir holds the helper and the service host.
func InstallDir() string { return Join(ProgramFiles(), "Twinnel") }

// HelperBin is the installed twinnel-helper.exe.
func HelperBin() string { return Join(InstallDir(), "twinnel-helper.exe") }

// SvcHostBin is the service host that runs mihomo.exe as a Windows
// service: a copy of twinnel-helper.exe, kept apart so removing or
// upgrading the helper never breaks the mihomo service.
func SvcHostBin() string { return Join(InstallDir(), "twinnel-svchost.exe") }

// StateDir is %ProgramData%\Twinnel.
func StateDir() string { return Join(ProgramData(), "Twinnel") }

// TailscaleDir is where the official Tailscale client installs itself.
func TailscaleDir() string { return Join(ProgramFiles(), "Tailscale") }

// TailscaleCLI is tailscale.exe.
func TailscaleCLI() string { return Join(TailscaleDir(), "tailscale.exe") }

// TailscaleDaemon is tailscaled.exe, run by the "Tailscale" service.
func TailscaleDaemon() string { return Join(TailscaleDir(), "tailscaled.exe") }

// Service names.
const (
	HelperService    = "TwinnelHelper"
	MihomoService    = "TwinnelMihomo"
	TailscaleService = "Tailscale"
	// Pipe is the helper's named pipe.
	Pipe = `\\.\pipe\twinnel-helper`
)

// SystemRoot is %SystemRoot%.
func SystemRoot() string { return env("SystemRoot", `C:\Windows`) }

// Netsh is the netsh.exe that sets adapter DNS servers.
func Netsh() string { return Join(SystemRoot(), "System32", "netsh.exe") }
