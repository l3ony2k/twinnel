//go:build !linux

package main

import "github.com/l3ony2k/twinnel/core/service"

// The socket group's membership is Linux's business (helper_linux.go):
// macOS's socket is for admin, Windows' pipe for the installing user's SID.

func planSocketAccess() []service.Step { return nil }

func reloginNote([]service.Step) bool { return false }
