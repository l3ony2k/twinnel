//go:build !unix

package kernel

import (
	"errors"
	"io/fs"
	"os/exec"
)

func fileOwner(fs.FileInfo) (int, int, bool) { return 0, 0, true }

func lookupCredential(int) (Credential, error) { return Credential{}, errors.New("not supported") }

func dropTo(*exec.Cmd, Credential) {}

// DropTo is a no-op on this platform.
func DropTo(*exec.Cmd, Credential) {}
