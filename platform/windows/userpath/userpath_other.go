//go:build !windows

package userpath

import "errors"

var errNotWindows = errors.New("the user PATH in the registry exists only on Windows")

// Has reports whether dir is on the user's PATH (Windows only).
func Has(string) (bool, error) { return false, errNotWindows }

// AddDir appends dir to the user's PATH (Windows only).
func AddDir(string) (bool, error) { return false, errNotWindows }

// RemoveDir takes dir off the user's PATH (Windows only).
func RemoveDir(string) (bool, error) { return false, errNotWindows }

// Broadcast does nothing outside Windows.
func Broadcast() {}
