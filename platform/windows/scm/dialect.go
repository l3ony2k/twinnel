package scm

import (
	"fmt"
	"strings"

	"github.com/l3ony2k/twinnel/core/service"
)

// Dialect is how plans read on Windows: cmd.exe's file commands (mkdir,
// copy, move, del, rmdir), the sc.exe service steps as an administrator
// could type them, Windows quoting, "as administrator" and "Twinnel".
// It is service.Current on Windows (dialect_windows.go); tests use it
// directly on any OS.
var Dialect = service.Dialect{
	Command:     windowsCommand,
	Note:        func(n string) string { return "  (" + n + ")" },
	Admin:       "administrator",
	App:         "Twinnel",
	ServiceName: "Windows service name",
}

func windowsCommand(s service.Step) string {
	q := EscapeArg
	switch s.Kind {
	case service.StepMkdir:
		return "mkdir " + q(s.Path)
	case service.StepCopy:
		return "copy /y " + q(s.Src) + " " + q(s.Path)
	case service.StepWrite:
		return fmt.Sprintf("write %s (%d bytes)", q(s.Path), len(s.Content))
	case service.StepRemove:
		return "del /f " + q(s.Path)
	case service.StepRemoveAll:
		return "rmdir /s /q " + q(s.Path)
	case service.StepRename:
		return "move /y " + q(s.Src) + " " + q(s.Path)
	case service.StepRun:
		return joinArgs(s.Argv)
	case service.StepWait:
		return service.WaitText(s, joinArgs(s.Argv))
	}
	return string(s.Kind)
}

// joinArgs is argv as one command line (CommandLine without its refusal
// of control characters: this only displays).
func joinArgs(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = EscapeArg(a)
	}
	return strings.Join(q, " ")
}
