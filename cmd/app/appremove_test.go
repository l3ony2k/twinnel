package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestAppRemovalScript(t *testing.T) {
	s, err := appRemovalScript(42, `C:\Users\o'neil\AppData\Local\Programs\Twinnel\`, `C:\Users\o'neil\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\Twinnel.lnk`,
		`C:\Users\o'neil\AppData\Local\Temp\twinnel-remove.log`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Wait-Process -Id 42",
		"Set-Location -LiteralPath $env:TEMP",
		`$dir = 'C:\Users\o''neil\AppData\Local\Programs\Twinnel'`,
		`$ps1 = 'C:\Users\o''neil\AppData\Local\Programs\Twinnel\install.ps1'`,
		`$log = 'C:\Users\o''neil\AppData\Local\Temp\twinnel-remove.log'`,
		// The install's own uninstall, which stops what still runs from
		// the folder (an agent's twinnel mcp) before removing it.
		"& $ps1 -Uninstall -InstallDir $dir *> $log",
		// Without install.ps1: the folder (only one with the marker) and the shortcut.
		`if (Test-Path -LiteralPath (Join-Path $dir '.twinnel-install')) { Remove-Item -LiteralPath $dir -Recurse -Force }`,
		`Twinnel.lnk' -Force`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	// The PATH is not rewritten here: [Environment]::SetEnvironmentVariable
	// would turn REG_EXPAND_SZ into REG_SZ (removeAppBundle uses userpath).
	if strings.Contains(s, "SetEnvironmentVariable") {
		t.Errorf("PATH rewritten by the script:\n%s", s)
	}
	for _, bad := range []string{`C:\`, `C:\Twinnel\..\Windows`, `C:\Program Files`, `/Users/x/Twinnel`, `\\server\share\Twinnel`, `Twinnel`, ""} {
		if _, err := appRemovalScript(42, bad, "", ""); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := appRemovalScript(0, `C:\Apps\Twinnel`, "", ""); err == nil {
		t.Error("pid 0 accepted")
	}
}

func TestEncodePowerShell(t *testing.T) {
	b, err := base64.StdEncoding.DecodeString(encodePowerShell("Write-Output 'é'"))
	if err != nil || len(b)%2 != 0 {
		t.Fatal(err)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	if got := string(utf16.Decode(u)); got != "Write-Output 'é'" {
		t.Fatalf("round trip: %q", got)
	}
}

func TestBundleFromExe(t *testing.T) {
	for _, c := range []struct{ goos, exe, want string }{
		{"darwin", "/Applications/Twinnel.app/Contents/MacOS/Twinnel", "/Applications/Twinnel.app"},
		{"darwin", "/tmp/go-build1/exe/app", ""},
		{"windows", `C:\Users\x\AppData\Local\Programs\Twinnel\Twinnel.exe`, `C:\Users\x\AppData\Local\Programs\Twinnel`},
		{"windows", `C:\dev\twinnel\twinnel.EXE`, `C:\dev\twinnel`},
		{"windows", `C:\Temp\go-build1\b001\exe\app.exe`, ""},
	} {
		if got := bundleFromExe(c.goos, c.exe); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.goos, c.exe, got, c.want)
		}
	}
}

func TestCmdQuoteWindows(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\x\AppData\Local\Programs\Twinnel\bin\twinnel.exe`: `C:\Users\x\AppData\Local\Programs\Twinnel\bin\twinnel.exe`,
		`C:\Program Files\Twinnel\bin\twinnel.exe`:                  `"C:\Program Files\Twinnel\bin\twinnel.exe"`,
		`C:\Users\a&b\twinnel.exe`:                                  `"C:\Users\a&b\twinnel.exe"`,
	} {
		if got := cmdQuoteFor("windows", in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
	if got := cmdQuoteFor("darwin", "/Applications/My Apps/Twinnel.app/x"); got != "'/Applications/My Apps/Twinnel.app/x'" {
		t.Errorf("posix: %s", got)
	}
}
