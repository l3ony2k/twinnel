//go:build !darwin

package main

import (
	"errors"

	"github.com/l3ony2k/duet/core/service"
)

func planHelperInstall(helperBin, stateDir, socket, label string) ([]service.Step, error) {
	return nil, errors.New("helper install is only implemented for macOS so far")
}
