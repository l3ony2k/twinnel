package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/setup"
)

type noTailnet struct{}

func (noTailnet) LoginState(context.Context) (setup.LoginState, error) {
	return setup.LoginState{}, errors.New("no tailscaled")
}
func (noTailnet) StartLogin(context.Context) error { return errors.New("no tailscaled") }

func setupFakes(t *testing.T) (*bytes.Buffer, *int) {
	oc, oi, ot, oo, oin, out := setupConnect, setupInstallHelper, setupTailnet, setupOpenURL, setupInteractive, stdout
	installs := 0
	var buf bytes.Buffer
	setupConnect = func(string) func(context.Context) (setup.Caller, error) {
		return func(context.Context) (setup.Caller, error) { return nil, errors.New("no socket") }
	}
	setupInstallHelper = func(context.Context) error { installs++; return errors.New("cancelled by the user") }
	setupTailnet = func() setup.Tailnet { return noTailnet{} }
	setupOpenURL = func(string) error { return nil }
	setupInteractive = func() bool { return false }
	stdout = &buf
	t.Cleanup(func() {
		setupConnect, setupInstallHelper, setupTailnet, setupOpenURL, setupInteractive, stdout = oc, oi, ot, oo, oin, out
	})
	t.Setenv("TWINNEL_SUBSCRIPTION_URL", "")
	return &buf, &installs
}

func TestSetupStatusChangesNothing(t *testing.T) {
	buf, installs := setupFakes(t)
	if err := runSetup([]string{"status", "--json"}); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Complete bool
		Steps    []setup.StepState
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("%v: %s", err, buf)
	}
	if out.Complete || len(out.Steps) != len(setup.Order) || out.Steps[0].State != setup.Pending || *installs != 0 {
		t.Fatalf("%+v installs %d", out, *installs)
	}
}

func TestSetupJSONReportsTheFailedStep(t *testing.T) {
	buf, installs := setupFakes(t)
	err := runSetup([]string{"--json", "--yes"})
	if err == nil || *installs != 1 {
		t.Fatalf("err %v installs %d", err, *installs)
	}
	var out struct {
		Steps []setup.StepState
		Error string
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("%v: %s", err, buf)
	}
	if out.Steps[0].State != setup.Failed || !strings.Contains(out.Steps[0].Error, "cancelled") || out.Steps[1].State != setup.Pending {
		t.Fatalf("%+v", out)
	}
}

func TestSetupImportRefusesGarbage(t *testing.T) {
	setupFakes(t)
	p := t.TempDir() + "/x.zip"
	if err := runSetup([]string{"--import", p}); err == nil {
		t.Fatal("imported a missing file")
	}
}
