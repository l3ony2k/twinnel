//go:build !windows

package winsec

import "errors"

var errNotWindows = errors.New("DPAPI and Windows ACLs only exist on Windows")

// Protect is DPAPI's CryptProtectData; only on Windows.
func Protect([]byte) ([]byte, error) { return nil, errNotWindows }

// Unprotect is DPAPI's CryptUnprotectData; only on Windows.
func Unprotect([]byte) ([]byte, error) { return nil, errNotWindows }

// SecureStateDir sets a Windows DACL; only on Windows.
func SecureStateDir(string) error { return errNotWindows }
