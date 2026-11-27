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

func setupFakes(t *testing.T) (*bytes.Buffer, *int) {
	oc, oi, oo, oin, out, ol := setupConnect, setupInstallHelper, setupOpenURL, setupInteractive, stdout, setupLinkTailscale
	setupLinkTailscale = func(string) (string, error) { return "", nil }
	installs := 0
	var buf bytes.Buffer
	setupConnect = func(string) func(context.Context) (setup.Caller, error) {
		return func(context.Context) (setup.Caller, error) { return nil, errors.New("no socket") }
	}
	setupInstallHelper = func(context.Context) error { installs++; return errors.New("cancelled by the user") }
	setupOpenURL = func(string) error { return nil }
	setupInteractive = func() bool { return false }
	stdout = &buf
	t.Cleanup(func() {
		setupConnect, setupInstallHelper, setupOpenURL, setupInteractive, stdout, setupLinkTailscale = oc, oi, oo, oin, out, ol
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

func TestURLOpener(t *testing.T) {
	u := "https://login.tailscale.com/a/synthetic&x=1"
	for goos, want := range map[string]string{
		"darwin":  "open " + u,
		"linux":   "xdg-open " + u,
		"windows": "rundll32.exe url.dll,FileProtocolHandler " + u,
	} {
		name, args := urlOpener(goos, u)
		if got := name + " " + strings.Join(args, " "); got != want {
			t.Errorf("%s: %q", goos, got)
		}
	}
}

func TestSetupStatusNeverClaimsComplete(t *testing.T) {
	buf, _ := setupFakes(t)
	if err := runSetup([]string{"status"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Setup complete") || !strings.Contains(buf.String(), "Setup not finished") {
		t.Fatalf("%s", buf)
	}
}
