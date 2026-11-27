//go:build windows

package privfiletest

import (
	"fmt"

	"github.com/l3ony2k/twinnel/core/privfile"
)

func check(path string, dir bool) error {
	got, err := privfile.Describe(path)
	if err != nil {
		return err
	}
	s, err := privfile.SDDL(dir)
	if err != nil {
		return err
	}
	want, err := privfile.Canonical(s)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%s:\n got  %s\n want %s", path, got, want)
	}
	return nil
}
