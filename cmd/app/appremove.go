package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// appRemovalScript is the PowerShell that deletes a Windows install of
// the app once process pid has exited (完全移除; files_windows.go): the
// folder, the Start menu shortcut, and the folder's bin from the user's
// PATH. It refuses a folder that is not an install of Twinnel: an
// absolute path named Twinnel, not a drive root.
func appRemovalScript(pid int, dir, shortcut string) (string, error) {
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
	fmt.Fprintf(&b, "if (Test-Path -LiteralPath %s) { Remove-Item -LiteralPath %s -Recurse -Force }\n", q(dir+`\`+installMarker), q(dir))
	if shortcut != "" {
		fmt.Fprintf(&b, "Remove-Item -LiteralPath %s -Force\n", q(shortcut))
	}
	fmt.Fprintf(&b, "$bin = %s\n", q(dir+`\bin`))
	b.WriteString("$p = [Environment]::GetEnvironmentVariable('Path', 'User')\n")
	b.WriteString("if ($p) { [Environment]::SetEnvironmentVariable('Path', (($p -split ';') | Where-Object { $_ -and ($_.TrimEnd('\\') -ne $bin) }) -join ';', 'User') }\n")
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
