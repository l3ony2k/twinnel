package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	coreversion "github.com/l3ony2k/twinnel/core/version"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
)

func fakeVersionSources(t *testing.T, h func() (rpc.Status, error), m func() (mihomo.Version, error), ts func() (string, error)) {
	oldH, oldM, oldT, oldP := helperStatus, mihomoVersion, tailscaleVersion, helperBinaryPaths
	helperStatus = func(context.Context, string) (rpc.Status, error) { return h() }
	mihomoVersion = func(context.Context, string, string) (mihomo.Version, error) { return m() }
	tailscaleVersion = func(context.Context) (string, error) { return ts() }
	// Never the machine's own helper binaries (TestVersionHelperDrift sets them).
	helperBinaryPaths = func() (string, string) { return "", "" }
	t.Cleanup(func() { helperStatus, mihomoVersion, tailscaleVersion, helperBinaryPaths = oldH, oldM, oldT, oldP })
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
	if rep.CLI.Version != coreversion.String() || rep.Helper.Version != "1.2.3" || rep.Mihomo.Version != "v1.19.0" || rep.Tailscale.Version != "1.90.0" {
		t.Fatalf("%+v", rep)
	}
	if !strings.Contains(rep.Mihomo.Detail, "managed kernel v1.19.0") {
		t.Errorf("mihomo detail: %q", rep.Mihomo.Detail)
	}
	var b bytes.Buffer
	writeVersions(&b, rep)
	for _, s := range []string{"twinnel         " + coreversion.String(), "twinnel-helper  1.2.3", "mihomo          v1.19.0", "tailscale       1.90.0"} {
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
	if !strings.Contains(b.String(), "twinnel-helper  -  (/x.sock; not reachable: no socket)") {
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

// The helper's two copies (Windows: bin\ beside the CLI and Program Files)
// show under twinnel-helper, and a difference is a warning.
func TestVersionHelperDrift(t *testing.T) {
	fakeVersionSources(t,
		func() (rpc.Status, error) { return rpc.Status{HelperVersion: "0.1.0"}, nil },
		func() (mihomo.Version, error) { return mihomo.Version{}, errors.New("refused") },
		func() (string, error) { return "", errors.New("no tailscaled") },
	)
	const bundled, installed = `C:\Users\u\AppData\Local\Programs\Twinnel\bin\twinnel-helper.exe`, `C:\Program Files\Twinnel\twinnel-helper.exe`
	helperBinaryPaths = func() (string, string) { return bundled, installed }
	versions := map[string]string{bundled: "0.2.0", installed: "0.1.0"}
	oldV := binaryVersion
	binaryVersion = func(_ context.Context, bin string) (string, error) { return versions[bin], nil }
	t.Cleanup(func() { binaryVersion = oldV })

	rep := collectVersions(context.Background(), time.Second, `\\.\pipe\twinnel-helper`, "127.0.0.1:9090", "")
	hb := rep.HelperBinaries
	if hb == nil || hb.Bundled.Version != "0.2.0" || hb.Installed.Version != "0.1.0" || !strings.Contains(hb.Warning, "twinnel helper upgrade") {
		t.Fatalf("%+v", hb)
	}
	var b bytes.Buffer
	writeVersions(&b, rep)
	for _, s := range []string{"  installed     0.1.0  (" + installed + ")", "  bundled       0.2.0  (" + bundled + ")", "warning: the installed helper (0.1.0) is older"} {
		if !strings.Contains(b.String(), s) {
			t.Errorf("output lacks %q:\n%s", s, b.String())
		}
	}
	var j map[string]any
	out, _ := json.Marshal(rep)
	json.Unmarshal(out, &j)
	if j["helper_binaries"] == nil {
		t.Errorf("json lacks helper_binaries: %s", out)
	}

	// Same binaries, running an old one: restart.
	versions[bundled], versions[installed] = "0.2.0", "0.2.0"
	rep = collectVersions(context.Background(), time.Second, "x", "127.0.0.1:9090", "")
	if w := rep.HelperBinaries.Warning; !strings.Contains(w, "running helper (0.1.0)") {
		t.Fatalf("warning = %q", w)
	}
	// All in step: no warning line.
	helperStatus = func(context.Context, string) (rpc.Status, error) { return rpc.Status{HelperVersion: "0.2.0"}, nil }
	rep = collectVersions(context.Background(), time.Second, "x", "127.0.0.1:9090", "")
	b.Reset()
	writeVersions(&b, rep)
	if rep.HelperBinaries.Warning != "" || strings.Contains(b.String(), "warning") {
		t.Fatalf("unexpected warning:\n%s", b.String())
	}
}
