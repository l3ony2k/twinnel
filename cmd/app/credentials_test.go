package main

import (
	"context"
	"errors"
	"testing"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

type credsHelper struct {
	state status.HelperState
	err   error
	creds rpc.ControllerCredentials
}

func (f *credsHelper) State() status.Helper { return status.Helper{State: f.state} }

func (f *credsHelper) Call(_ context.Context, method string, _, result any) error {
	if method != rpc.MethodControllerCredentials {
		return errors.New("unexpected " + method)
	}
	if f.err != nil {
		return f.err
	}
	*(result.(*rpc.ControllerCredentials)) = f.creds
	return nil
}

func TestFetchCredentials(t *testing.T) {
	ctx := context.Background()
	f := &credsHelper{state: status.HelperConnected, creds: rpc.ControllerCredentials{Controller: "127.0.0.1:19190", Secret: "s2"}}
	c, old, err := fetchCredentials(ctx, f)
	if err != nil || old || c == nil || c.Secret != "s2" {
		t.Fatalf("connected: %v %v %v", c, old, err)
	}
	f.state = status.HelperNotRunning
	if c, old, err := fetchCredentials(ctx, f); c != nil || old || err != nil {
		t.Fatalf("not connected: %v %v %v", c, old, err)
	}
	f.state, f.err = status.HelperConnected, &rpc.Error{Code: rpc.CodeMethodNotFound}
	if c, old, err := fetchCredentials(ctx, f); c != nil || !old || err != nil {
		t.Fatalf("old helper: %v %v %v", c, old, err)
	}
	f.err = &rpc.Error{Code: rpc.CodeFailed, Message: "boom"}
	if _, _, err := fetchCredentials(ctx, f); err == nil {
		t.Fatal("error swallowed")
	}
	if c, _, _ := fetchCredentials(ctx, nil); c != nil {
		t.Fatal("nil conn")
	}
}

func TestControllerConfig(t *testing.T) {
	cfg := status.Config{Controller: "127.0.0.1:9090", Secret: "typed", PrimaryGroup: "G"}
	if got := controllerConfig(cfg, nil); got.Secret != "typed" || got.Controller != "127.0.0.1:9090" {
		t.Fatalf("no creds: %+v", got)
	}
	got := controllerConfig(cfg, &rpc.ControllerCredentials{Controller: "127.0.0.1:19190", Secret: ""})
	if got.Secret != "" || got.Controller != "127.0.0.1:19190" || got.PrimaryGroup != "G" {
		t.Fatalf("creds (empty secret wins too): %+v", got)
	}
	if got := controllerConfig(cfg, &rpc.ControllerCredentials{Secret: "x"}); got.Controller != "127.0.0.1:9090" || got.Secret != "x" {
		t.Fatalf("no address: %+v", got)
	}
	if !sameCreds(nil, nil) || sameCreds(nil, &rpc.ControllerCredentials{}) || !sameCreds(&rpc.ControllerCredentials{Secret: "a"}, &rpc.ControllerCredentials{Secret: "a", Source: "x"}) {
		t.Fatal("sameCreds")
	}
	if !credentialsEvent([]rpc.Event{{Type: rpc.EventComposeApplied}}) || credentialsEvent([]rpc.Event{{Type: rpc.EventTakeoverChanged}}) {
		t.Fatal("credentialsEvent")
	}
}
