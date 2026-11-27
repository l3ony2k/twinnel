package main

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/store"
)

// doctorBinaries are the programs `twinnel doctor` checks for damage
// (binary-integrity): the managed mihomo in the helper's state dir and
// the Tailscale binaries. A file that is not there is left out; one this
// process may not read is kept, so the report says "access denied", not
// "missing" (the state dir is for administrators only on Windows). A
// variable for tests.
var doctorBinaries = func() []health.BinaryRef {
	var out []health.BinaryRef
	add := func(name, p string, signed bool) {
		if p == "" {
			return
		}
		if _, err := os.Stat(p); err != nil && !errors.Is(err, fs.ErrPermission) {
			return
		}
		out = append(out, health.BinaryRef{Name: name, Path: p, Signed: signed})
	}
	state := store.DefaultDir
	if runtime.GOOS == "linux" {
		state = "/var/lib/twinnel"
	}
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	add("mihomo", filepath.Join(state, "kernels", "mihomo", "current", "mihomo"+exe), false)
	if runtime.GOOS == "windows" {
		pf := os.Getenv("ProgramFiles")
		if len(pf) < 3 || pf[1] != ':' {
			pf = `C:\Program Files`
		}
		dir := strings.TrimRight(pf, `\`) + `\Tailscale`
		add("tailscale", dir+`\tailscale.exe`, true)
		add("tailscaled", dir+`\tailscaled.exe`, true)
		return out
	}
	for _, n := range []string{"tailscale", "tailscaled"} {
		if p, err := exec.LookPath(n); err == nil {
			add(n, p, false)
		}
	}
	return out
}
