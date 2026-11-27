package main

import (
	"path/filepath"
	"testing"
)

// The Windows 命令行工具 row reads the user's PATH through userPath (faked
// here: tests never touch the registry) for the folder holding the CLI.
func TestCLIPathRefresh(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TWINNEL_CLI", filepath.Join(dir, "twinnel.exe"))
	old := userPath
	t.Cleanup(func() { userPath = old })
	var asked string
	userPath.Has = func(d string) (bool, error) { asked = d; return true, nil }

	a := &app{}
	a.refreshCLIPath()
	if !a.agent.pathRead || !a.agent.onPath || a.agent.binDir != dir || asked != dir {
		t.Fatalf("agent = %+v, asked %q", a.agent, asked)
	}
	// Shot runs never read the machine.
	asked = ""
	b := &app{shotMode: true}
	b.refreshCLIPath()
	if asked != "" || !b.agent.pathRead {
		t.Fatalf("shot mode read the PATH: %q", asked)
	}
}
