// Package privfiletest checks, in other packages' tests, that a file or
// dir was written private (core/privfile): mode 0600/0700 on Unix; on
// Windows the owner-only protected DACL, compared as SDDL.
package privfiletest

import "testing"

// AssertPrivate fails t unless path is private (dir: a private dir).
func AssertPrivate(t testing.TB, path string, dir bool) {
	t.Helper()
	if err := check(path, dir); err != nil {
		t.Error(err)
	}
}
