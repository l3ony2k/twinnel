package windows

import "strings"

// exeOf is the program of a Run key command line: the quoted path, or
// everything up to ".exe".
func exeOf(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if i := strings.Index(cmd[1:], `"`); i >= 0 {
			return cmd[1 : i+1]
		}
	}
	if i := strings.Index(strings.ToLower(cmd), ".exe"); i >= 0 {
		return cmd[:i+4]
	}
	prog, _, _ := strings.Cut(cmd, " ")
	return prog
}
