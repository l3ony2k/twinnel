//go:build !windows

package winsec

import "errors"

// VerifyAuthenticode is Windows only.
func VerifyAuthenticode(string) error { return errors.New("authenticode: not on this platform") }
