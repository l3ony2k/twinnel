package rpc

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Well-known SIDs.
const (
	SIDLocalSystem    = "S-1-5-18"
	SIDAdministrators = "S-1-5-32-544"
)

var sidRE = regexp.MustCompile(`^S-1-[0-9]+(-[0-9]+){1,14}$`)

// ValidSID reports whether s is a SID in string form ("S-1-5-21-…").
// SIDs end up in an SDDL string, so this is also the injection guard.
func ValidSID(s string) bool { return sidRE.MatchString(s) }

// PipeClientAccess is what a client opens the helper's pipe with:
// FILE_GENERIC_READ | FILE_WRITE_DATA | FILE_WRITE_ATTRIBUTES |
// FILE_WRITE_EA, i.e. GENERIC_READ|GENERIC_WRITE without FILE_APPEND_DATA,
// which on a pipe is FILE_CREATE_PIPE_INSTANCE. Allowed users get exactly
// this, so they can talk to the helper but never create an instance of its
// pipe (squat it) while it is down.
const PipeClientAccess = 0x0012019b

// PipeSDDL is the security descriptor of the helper's pipe: LocalSystem and
// Administrators full access, and each allowed SID (the user who installed
// the helper; the helper's own user when it is not LocalSystem, as a
// development helper) client access. Protected: nothing is inherited. This
// is the macOS rule (root, the helper's uid, the admin group) in Windows
// terms: an admin's normal, filtered token has Administrators deny-only, so
// the installing user's own SID is what lets the app connect without a UAC
// prompt. Invalid SIDs are an error.
func PipeSDDL(allowed ...string) (string, error) {
	var b strings.Builder
	b.WriteString("D:P(A;;GA;;;SY)(A;;GA;;;BA)")
	seen := map[string]bool{SIDLocalSystem: true, SIDAdministrators: true}
	for _, s := range allowed {
		if s == "" || seen[s] {
			continue
		}
		if !ValidSID(s) {
			return "", fmt.Errorf("invalid SID %q", s)
		}
		seen[s] = true
		fmt.Fprintf(&b, "(A;;0x%08x;;;%s)", PipeClientAccess, s)
	}
	return b.String(), nil
}

// SIDAuthorizer is the Windows rule, checked on every connection on top of
// the pipe's DACL: LocalSystem, the helper's own user, an allowed SID, or an
// elevated token with Administrators enabled.
type SIDAuthorizer struct {
	SelfSID string
	Allowed []string
}

// Authorize implements Authorizer.
func (a *SIDAuthorizer) Authorize(p Peer) error {
	switch {
	case p.SID == "":
		return fmt.Errorf("peer has no SID")
	case p.SID == SIDLocalSystem, a.SelfSID != "" && p.SID == a.SelfSID, slices.Contains(a.Allowed, p.SID):
		return nil
	case p.Elevated && p.Admin:
		return nil
	}
	return fmt.Errorf("%s is neither an allowed user nor an elevated administrator", p.SID)
}
