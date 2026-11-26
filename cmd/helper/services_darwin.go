//go:build darwin

package main

import (
	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/platform/darwin/launchd"
)

func platformServices() service.Manager { return launchd.New() }
