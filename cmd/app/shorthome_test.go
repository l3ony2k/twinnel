package main

import "testing"

// 6.13: a path under the home folder is short in the system's idiom: ~ on
// macOS and Linux, the known folder variable on Windows (never ~).
func TestShortHome(t *testing.T) {
	env := map[string]string{
		"LOCALAPPDATA": `C:\Users\example\AppData\Local`,
		"APPDATA":      `C:\Users\example\AppData\Roaming`,
		"USERPROFILE":  `C:\Users\example`,
	}
	getenv := func(k string) string { return env[k] }
	for _, c := range []struct{ goos, p, want string }{
		{"windows", `C:\Users\example\AppData\Local\Programs\Twinnel`, `%LOCALAPPDATA%\Programs\Twinnel`},
		{"windows", `c:\users\EXAMPLE\appdata\roaming\Twinnel\x.json`, `%APPDATA%\Twinnel\x.json`},
		{"windows", `C:\Users\example\.config\mihomo`, `%USERPROFILE%\.config\mihomo`},
		{"windows", `C:\Users\example2\x`, `C:\Users\example2\x`},
		{"windows", `C:\Program Files\Twinnel`, `C:\Program Files\Twinnel`},
		{"darwin", "/Users/example/.config/mihomo", "~/.config/mihomo"},
		{"linux", "/home/example/.local/bin/twinnel", "/home/example/.local/bin/twinnel"},
	} {
		home := "/Users/example"
		if got := shortHome(c.p, c.goos, home, getenv); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.goos, c.p, got, c.want)
		}
	}
}
