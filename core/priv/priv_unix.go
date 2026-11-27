//go:build !windows

package priv

import "os"

func elevated() bool { return os.Geteuid() == 0 }

// CurrentUserSID is a Windows concept; elsewhere it is empty.
func CurrentUserSID() (string, error) { return "", nil }
