package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// appRemovalScript is the PowerShell that deletes a Windows install of
// the app once process pid has exited (完全移除; files_windows.go). It runs
// the install's own install.ps1 -Uninstall, the one way a Windows install
// is taken apart: it stops Twinnel's programs still running from the folder
// (an agent's `twinnel mcp` keeps bin\twinnel.exe open, and a running
// program cannot be deleted), then removes the shortcut, the PATH entry,
// the start-at-login entry and the folder, and writes what it did to log.
// A folder without install.ps1 gets the folder and the shortcut removed
// here. It refuses a folder that is not an install of Twinnel: an
// absolute path named Twinnel, not a drive root.
func appRemovalScript(pid int, dir, shortcut, log string) (string, error) {
	dir = strings.TrimRight(dir, `\`)
	if !safeAppDir(dir) {
		return "", fmt.Errorf("not a Twinnel install folder: %q", dir)
	}
	if pid <= 0 {
		return "", errors.New("no process to wait for")
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	var b strings.Builder
	fmt.Fprintf(&b, "$ErrorActionPreference = 'SilentlyContinue'\n")
	fmt.Fprintf(&b, "Wait-Process -Id %d -Timeout 120\n", pid)
	fmt.Fprintf(&b, "Start-Sleep -Milliseconds 500\n")
	// Not from inside the folder: a current directory keeps it from going.
	b.WriteString("Set-Location -LiteralPath $env:TEMP; [Environment]::CurrentDirectory = $env:TEMP\n")
	fmt.Fprintf(&b, "$dir = %s\n", q(dir))
	fmt.Fprintf(&b, "$ps1 = %s\n", q(dir+`\install.ps1`))
	if log != "" {
		fmt.Fprintf(&b, "$log = %s\n", q(log))
	} else {
		b.WriteString("$log = $null\n")
	}
	b.WriteString("if ((Test-Path -LiteralPath (Join-Path $dir '" + installMarker + "')) -and (Test-Path -LiteralPath $ps1)) {\n")
	b.WriteString("\tif ($log) { & $ps1 -Uninstall -InstallDir $dir *> $log } else { & $ps1 -Uninstall -InstallDir $dir }\n")
	b.WriteString("\texit\n}\n")
	fmt.Fprintf(&b, "if (Test-Path -LiteralPath (Join-Path $dir '%s')) { Remove-Item -LiteralPath $dir -Recurse -Force }\n", installMarker)
	if shortcut != "" {
		fmt.Fprintf(&b, "Remove-Item -LiteralPath %s -Force\n", q(shortcut))
	}
	return b.String(), nil
}

// safeAppDir: X:\…\Twinnel, at least one folder below the drive root.
func safeAppDir(dir string) bool {
	if len(dir) < 4 || dir[1] != ':' || dir[2] != '\\' || strings.Contains(dir, "/") {
		return false
	}
	c := dir[0] | 0x20
	if c < 'a' || c > 'z' {
		return false
	}
	i := strings.LastIndex(dir, `\`)
	return i > 2 && strings.EqualFold(dir[i+1:], "Twinnel") && !strings.Contains(dir, `\..`)
}

// encodePowerShell is a script for -EncodedCommand: base64 of UTF-16LE,
// which needs no quoting on the command line.
func encodePowerShell(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(u))
	for i, r := range u {
		b[2*i], b[2*i+1] = byte(r), byte(r>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}
