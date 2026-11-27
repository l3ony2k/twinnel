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

func serviceInfo(context.Context, string) (string, bool, int, error) {
	return "", false, 0, errors.New("the Service Control Manager only exists on Windows")
}

func processImage(int) string { return "" }
