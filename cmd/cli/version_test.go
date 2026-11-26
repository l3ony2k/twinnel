package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/rpc"
)

func fakeVersionSources(t *testing.T, h func() (rpc.Status, error), m func() (mihomo.Version, error), ts func() (string, error)) {
	oldH, oldM, oldT := helperStatus, mihomoVersion, tailscaleVersion
	helperStatus = func(context.Context, string) (rpc.Status, error) { return h() }
	mihomoVersion = func(context.Context, string, string) (mihomo.Version, error) { return m() }
	tailscaleVersion = func(context.Context) (string, error) { return ts() }
	t.Cleanup(func() { helperStatus, mihomoVersion, tailscaleVersion = oldH, oldM, oldT })
}

func TestVersionAllReachable(t *testing.T) {
	fakeVersionSources(t,
		func() (rpc.Status, error) {
			return rpc.Status{HelperVersion: "1.2.3", Kernel: rpc.KernelList{Active: "/s/kernels/v1.19.0/mihomo", Current: "v1.19.0"}}, nil
		},
		func() (mihomo.Version, error) { return mihomo.Version{Meta: true, Version: "v1.19.0"}, nil },
		func() (string, error) { return "1.90.0", nil },
	)
	rep := collectVersions(context.Background(), time.Second, "/x.sock", "127.0.0.1:9090", "")
	if rep.CLI.Version != version || rep.Helper.Version != "1.2.3" || rep.Mihomo.Version != "v1.19.0" || rep.Tailscale.Version != "1.90.0" {
		t.Fatalf("%+v", rep)
	}
	if !strings.Contains(rep.Mihomo.Detail, "managed kernel v1.19.0") {
		t.Errorf("mihomo detail: %q", rep.Mihomo.Detail)
	}
	var b bytes.Buffer
	writeVersions(&b, rep)
	for _, s := range []string{"duet         " + version, "duet-helper  1.2.3", "mihomo       v1.19.0", "tailscale    1.90.0"} {
		if !strings.Contains(b.String(), s) {
			t.Errorf("output lacks %q:\n%s", s, b.String())
		}
	}
}

// Nothing reachable: still prints the CLI version, names each failure.
func TestVersionNothingReachable(t *testing.T) {
	fakeVersionSources(t,
		func() (rpc.Status, error) { return rpc.Status{}, errors.New("no socket") },
		func() (mihomo.Version, error) { return mihomo.Version{}, errors.New("refused") },
		func() (string, error) { return "", errors.New("no tailscaled") },
	)
	rep := collectVersions(context.Background(), time.Second, "/x.sock", "127.0.0.1:9090", "")
	if rep.CLI.Version == "" || rep.Helper.Error != "no socket" || rep.Mihomo.Error != "refused" || rep.Tailscale.Error != "no tailscaled" {
		t.Fatalf("%+v", rep)
	}
	var b bytes.Buffer
	writeVersions(&b, rep)
	if !strings.Contains(b.String(), "duet-helper  -  (/x.sock; not reachable: no socket)") {
		t.Errorf("%s", b.String())
	}
	if _, err := json.Marshal(rep); err != nil {
		t.Fatal(err)
	}
}

// The helper reports mihomo's version when this user cannot reach the
// controller (e.g. the secret is only in the helper).
func TestVersionMihomoViaHelper(t *testing.T) {
	fakeVersionSources(t,
		func() (rpc.Status, error) {
			return rpc.Status{HelperVersion: "1", Mihomo: rpc.MihomoStatus{Version: "v1.18.0", Controller: "127.0.0.1:9090"}}, nil
		},
		func() (mihomo.Version, error) { return mihomo.Version{}, errors.New("401") },
		func() (string, error) { return "1.90.0", nil },
	)
	rep := collectVersions(context.Background(), time.Second, "/x.sock", "127.0.0.1:9090", "")
	if rep.Mihomo.Version != "v1.18.0" || rep.Mihomo.Error != "" || !strings.Contains(rep.Mihomo.Detail, "via the helper") {
		t.Fatalf("%+v", rep.Mihomo)
	}
}
