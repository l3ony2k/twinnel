//go:build !windows

package main

import (
	"github.com/l3ony2k/twinnel/core/priv"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
)

// listenTransport: a unix socket owned by the --group (as root), with the
// group rule.
func listenTransport(o *options) (rpc.Transport, rpc.Authorizer, error) {
	tr := rpc.NewUnixTransport()
	auth, err := rpc.NewGroupAuthorizer(o.group)
	if err != nil {
		return nil, nil, err
	}
	if priv.Elevated() {
		tr.GID = auth.GID
	}
	return tr, auth, nil
}

// runService: only Windows has a service mode.
func runService(*options) (bool, error) { return false, nil }

// platformExecutor runs plan commands with os/exec.
func platformExecutor() service.Executor { return service.ExecExecutor{} }

// prepareStateDir: store.Open creates and checks the state dir on Unix.
func prepareStateDir(string) error { return nil }
