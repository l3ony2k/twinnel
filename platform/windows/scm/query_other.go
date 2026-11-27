//go:build !windows

package scm

import (
	"context"
	"errors"

	"github.com/l3ony2k/twinnel/core/service"
)

func queryService(context.Context, string) (service.Status, error) {
	return service.Status{}, errors.New("the Service Control Manager only exists on Windows")
}

// ServiceCommandLine is an installed service's command line; Windows only.
func ServiceCommandLine(string) (string, error) {
	return "", errors.New("the Service Control Manager only exists on Windows")
}
