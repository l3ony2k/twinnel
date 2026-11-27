//go:build windows

package priv

import "golang.org/x/sys/windows"

// elevated: the process token is elevated (UAC "Run as administrator") or
// belongs to LocalSystem (the helper's service account).
func elevated() bool {
	t := windows.GetCurrentProcessToken()
	if t.IsElevated() {
		return true
	}
	u, err := t.GetTokenUser()
	if err != nil {
		return false
	}
	return u.User.Sid.IsWellKnown(windows.WinLocalSystemSid)
}

// CurrentUserSID is the SID of the process's user, e.g. "S-1-5-21-…-1001".
func CurrentUserSID() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}
