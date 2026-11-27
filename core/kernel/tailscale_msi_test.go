package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeMSIHost is pkgs.tailscale.com with synthetic MSIs.
type fakeMSIHost struct {
	*httptest.Server
	msi       map[string][]byte // arch -> bytes
	badSum    bool
	downloads int
}

func newFakeMSIHost(t *testing.T) *fakeMSIHost {
	f := &fakeMSIHost{msi: map[string][]byte{
		"amd64": []byte("synthetic msi amd64"),
		"arm64": []byte("synthetic msi arm64"),
		"x86":   []byte("synthetic msi x86"),
	}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stable/" {
			msis := map[string]string{}
			for arch := range f.msi {
				msis[arch] = "tailscale-setup-1.104.7-" + arch + ".msi"
			}
			json.NewEncoder(w).Encode(map[string]any{"MSIsVersion": "1.104.7", "MSIs": msis,
				"Tarballs": map[string]string{"amd64": "tailscale_1.104.7_amd64.tgz"}})
			return
		}
		for arch, b := range f.msi {
			name := "/stable/tailscale-setup-1.104.7-" + arch + ".msi"
			switch r.URL.Path {
			case name:
				f.downloads++
				w.Write(b)
				return
			case name + ".sha256":
				s := sum(b)
				if f.badSum {
					s = strings.Repeat("0", 64)
				}
				fmt.Fprintln(w, s)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

func TestTailscaleResolveMSI(t *testing.T) {
	f := newFakeMSIHost(t)
	for goarch, arch := range map[string]string{"amd64": "amd64", "arm64": "arm64", "386": "x86"} {
		in := &TailscaleInstaller{Dir: t.TempDir(), PkgsURL: f.URL, GOOS: "windows", GOARCH: goarch}
		if !in.UsesMSI() {
			t.Fatal("windows does not use the MSI")
		}
		a, err := in.Resolve(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if a.Name != "tailscale-setup-1.104.7-"+arch+".msi" || a.Version != "1.104.7" || a.Source != TSSourceMSI || a.SHA256 != sum(f.msi[arch]) {
			t.Fatalf("%s: %+v", goarch, a)
		}
	}
	delete(f.msi, "arm64")
	in := &TailscaleInstaller{Dir: t.TempDir(), PkgsURL: f.URL, GOOS: "windows", GOARCH: "arm64"}
	if _, err := in.Resolve(context.Background()); err == nil || !strings.Contains(err.Error(), "no Windows installer for arm64") {
		t.Fatalf("missing arch: %v", err)
	}
	if (&TailscaleInstaller{GOOS: "linux"}).UsesMSI() {
		t.Fatal("linux uses the MSI")
	}
}

func TestTailscaleInstallMSI(t *testing.T) {
	f := newFakeMSIHost(t)
	dir := t.TempDir()
	winDir := filepath.Join(t.TempDir(), "Tailscale")
	var ran []string
	var probed string
	in := &TailscaleInstaller{Dir: dir, PkgsURL: f.URL, GOOS: "windows", GOARCH: "amd64", WindowsDir: winDir,
		RunMSI: func(_ context.Context, msi, log string) (int, error) {
			b, err := os.ReadFile(msi)
			if err != nil || string(b) != "synthetic msi amd64" {
				t.Errorf("msi %s: %q %v", msi, b, err)
			}
			if filepath.Dir(filepath.Dir(msi)) != dir {
				t.Errorf("msi outside the state dir: %s", msi)
			}
			ran = append(ran, filepath.Base(msi), log)
			return 0, nil
		},
		Probe: func(_ context.Context, cli string) (string, error) { probed = cli; return "1.104.7", nil },
	}
	ctx := context.Background()
	a, err := in.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := in.InstallMSI(ctx, a, `C:\ProgramData\Twinnel\tailscale-msi.log`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.104.7" || got.CLI != filepath.Join(winDir, "tailscale.exe") || got.Daemon != filepath.Join(winDir, "tailscaled.exe") || got.RestartNeeded || probed != got.CLI {
		t.Fatalf("installed %+v (probed %s)", got, probed)
	}
	if !slices.Equal(ran, []string{"tailscale-setup-1.104.7-amd64.msi", `C:\ProgramData\Twinnel\tailscale-msi.log`}) {
		t.Fatalf("ran %v", ran)
	}
	// The download is gone afterwards.
	ents, _ := os.ReadDir(dir)
	if len(ents) != 0 {
		t.Fatalf("left behind %v", ents)
	}

	// 3010: installed, restart pending.
	in.RunMSI = func(context.Context, string, string) (int, error) { return 3010, nil }
	if got, err := in.InstallMSI(ctx, a, "log"); err != nil || !got.RestartNeeded {
		t.Fatalf("3010: %+v %v", got, err)
	}
	// Failures say what to do, in words.
	for code, want := range map[int]string{
		1618: "another installation is in progress",
		1625: "a system policy blocks installing Tailscale",
		1603: "the Tailscale installer failed (msiexec exit code 1603); its log is log",
	} {
		in.RunMSI = func(context.Context, string, string) (int, error) { return code, nil }
		_, err := in.InstallMSI(ctx, a, "log")
		var me *MSIExitError
		if !errors.As(err, &me) || me.Code != code || !strings.Contains(err.Error(), want) {
			t.Errorf("%d: %v", code, err)
		}
	}
	// Installed but the CLI does not run.
	in.RunMSI = func(context.Context, string, string) (int, error) { return 0, nil }
	in.Probe = func(context.Context, string) (string, error) { return "", errors.New("exec: not found") }
	if _, err := in.InstallMSI(ctx, a, "log"); err == nil || !strings.Contains(err.Error(), "does not run") {
		t.Fatalf("probe failure: %v", err)
	}
}

func TestTailscaleInstallMSIRefuses(t *testing.T) {
	f := newFakeMSIHost(t)
	ran := false
	in := &TailscaleInstaller{Dir: t.TempDir(), PkgsURL: f.URL, GOOS: "windows", GOARCH: "amd64",
		RunMSI: func(context.Context, string, string) (int, error) { ran = true; return 0, nil },
		Probe:  func(context.Context, string) (string, error) { return "1.104.7", nil }}
	ctx := context.Background()
	a, err := in.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]TailscaleAsset{
		"foreign host": {Version: a.Version, Name: a.Name, URL: "https://evil.example.com/" + a.Name, SHA256: a.SHA256},
		"no sha256":    {Version: a.Version, Name: a.Name, URL: a.URL},
		"not an msi":   {Version: a.Version, Name: "setup.exe", URL: f.URL + "/stable/setup.exe", SHA256: a.SHA256},
		"bad version":  {Version: "1.104", Name: a.Name, URL: a.URL, SHA256: a.SHA256},
	} {
		if _, err := in.InstallMSI(ctx, bad, "log"); err == nil {
			t.Errorf("%s: installed", name)
		}
	}
	f.badSum = true
	a, err = in.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.InstallMSI(ctx, a, "log"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("bad sum: %v", err)
	}
	if ran {
		t.Fatal("msiexec ran on a refused download")
	}
}

func TestMSIArgs(t *testing.T) {
	got := MSIArgs(`C:\ProgramData\Twinnel\tailscale\.install-1\tailscale-setup-1.104.7-amd64.msi`, `C:\ProgramData\Twinnel\tailscale-msi.log`)
	want := []string{"/i", `C:\ProgramData\Twinnel\tailscale\.install-1\tailscale-setup-1.104.7-amd64.msi`, "/qn", "/norestart",
		"/l*v", `C:\ProgramData\Twinnel\tailscale-msi.log`, "TS_NOLAUNCH=true"}
	if !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
	t.Setenv("SystemRoot", `D:\Win`)
	if MSIExec() != `D:\Win\System32\msiexec.exe` {
		t.Fatal(MSIExec())
	}
	t.Setenv("SystemRoot", "relative")
	if MSIExec() != `C:\Windows\System32\msiexec.exe` {
		t.Fatal(MSIExec())
	}
}
