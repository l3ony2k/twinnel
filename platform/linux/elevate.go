package linux

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Pkexec is polkit's pkexec: it asks the session's authentication agent
// (KDE's, GNOME's, hyprpolkitagent, polkit-gnome…) for the password of an
// administrator, then runs the program as root.
const Pkexec = "/usr/bin/pkexec"

// PkexecArgv is the command that runs exe with args as root through
// pkexec. Every word is its own argv element: no shell sees them, so no
// quoting is needed and none is done. pkexec wants an absolute program
// path, and refuses words with control characters here, as the macOS
// path does, so a value never reaches root that the plan shown before
// did not print.
func PkexecArgv(exe string, args []string) ([]string, error) {
	if !filepath.IsAbs(exe) {
		return nil, fmt.Errorf("pkexec needs an absolute program path, not %q", exe)
	}
	for _, a := range append([]string{exe}, args...) {
		if strings.ContainsFunc(a, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return nil, fmt.Errorf("refusing argument with control characters: %q", a)
		}
	}
	return append([]string{Pkexec, exe}, args...), nil
}

// ErrNoAuthAgent is a pkexec failure for want of an authentication agent
// in the session (a bare Wayland compositor without one started).
var ErrNoAuthAgent = errors.New("no polkit authentication agent is running in this session: start one (hyprpolkitagent, polkit-kde-agent, polkit-gnome) or run `sudo twinnel helper install`")

// PkexecFailure reads a failed pkexec run: 126 is the dialog dismissed
// (cancelled), 127 is no authorization (refused, or no agent to ask);
// any other code is the program's own exit, whose last "twinnel: " line
// on stderr is its error. known is true for pkexec's own codes.
func PkexecFailure(code int, stderr string) (msg string, cancelled, known bool) {
	switch code {
	case 126:
		return "", true, true
	case 127:
		if strings.Contains(stderr, "No authentication agent") || strings.Contains(stderr, "no authentication agent") {
			return ErrNoAuthAgent.Error(), false, true
		}
		if strings.Contains(stderr, "Not authorized") {
			return "not authorized: polkit refused the request", false, true
		}
		return "", false, true
	}
	return lastTwinnelError(stderr), false, false
}

// lastTwinnelError is the text of the last "twinnel: " line in s, "" for
// none.
func lastTwinnelError(s string) string {
	msg := ""
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "twinnel: "); i >= 0 {
			msg = strings.TrimSpace(line[i+len("twinnel: "):])
		}
	}
	return msg
}
