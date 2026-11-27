package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/kernel"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
)

// msiHost is pkgs.tailscale.com with one synthetic amd64 MSI.
func msiHost(t *testing.T) *httptest.Server {
	msi := []byte("synthetic tailscale msi")
	sum := sha256.Sum256(msi)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stable/":
			json.NewEncoder(w).Encode(map[string]any{"MSIs": map[string]string{"amd64": "tailscale-setup-1.104.7-amd64.msi"}})
		case "/stable/tailscale-setup-1.104.7-amd64.msi":
			w.Write(msi)
		case "/stable/tailscale-setup-1.104.7-amd64.msi.sha256":
			fmt.Fprintln(w, hex.EncodeToString(sum[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// scmFake is the SCM: the Tailscale service appears after RunMSI ran and
// runs after a few polls.
type scmFake struct {
	fakeManager
	installed bool
	polls     int
	runAfter  int // -1: never runs
}

func (m *scmFake) Status(_ context.Context, l string) (service.Status, error) {
	if !m.installed {
		return service.Status{Label: l}, nil
	}
	m.polls++
	return service.Status{Label: l, Installed: true, Loaded: true, Running: m.runAfter >= 0 && m.polls > m.runAfter}, nil
}

func TestTailscaleInstallWindowsMSI(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	srv := msiHost(t)
	scm := &scmFake{runAfter: 2}
	var msiRuns []string
	h.e.Services = scm
	h.e.ServiceRunner = &ServiceRunner{Exec: &recExec{}, Files: &recFiles{}}
	h.e.Sleep = func(context.Context, time.Duration) error { return nil }
	h.e.TSProbe = &kernel.TailscaleProbe{Stat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }}
	winDir := filepath.Join(t.TempDir(), "Tailscale")
	h.e.TSInstaller = &kernel.TailscaleInstaller{Dir: h.e.Store.Path("tailscale"), PkgsURL: srv.URL, GOOS: "windows", GOARCH: "amd64",
		WindowsDir: winDir,
		RunMSI: func(_ context.Context, msi, log string) (int, error) {
			msiRuns = append(msiRuns, filepath.Base(msi))
			if log != h.e.Store.Path("tailscale-msi.log") {
				t.Errorf("log %s", log)
			}
			scm.installed = true
			return 0, nil
		},
		Probe: func(context.Context, string) (string, error) { return "1.104.7", nil }}

	r, err := h.e.TailscaleInstall(ctx, rpc.TailscaleInstallParams{Proxy: rpc.ProxyDirect})
	if err != nil {
		t.Fatal(err)
	}
	if r.Kept || r.Kind != kernel.TSWindows || r.Version != "1.104.7" || r.Path != filepath.Join(winDir, "tailscale.exe") ||
		r.Source != kernel.TSSourceMSI || r.Service == nil || r.Service.DryRun || !r.Service.Status.Running || len(msiRuns) != 1 {
		t.Fatalf("result %+v (msi runs %v)", r, msiRuns)
	}
	if !strings.Contains(strings.Join(r.Service.Steps, "\n"), "TS_NOLAUNCH=true") {
		t.Fatalf("steps %q", r.Service.Steps)
	}
	// No service definition is written: the service is the MSI's.
	if len(scm.specs) != 0 {
		t.Fatalf("planned a service: %+v", scm.specs)
	}
	st, _ := h.e.Store.Settings()
	if st.Tailscale.Binary != r.Path || st.Tailscale.DaemonBinary != filepath.Join(winDir, "tailscaled.exe") || !st.Tailscale.ServiceAdopted {
		t.Fatalf("settings %+v", st.Tailscale)
	}

	// The service never starts: a plain error, not a hang.
	scm.installed, scm.polls, scm.runAfter = false, 0, -1
	_, err = h.e.TailscaleInstall(ctx, rpc.TailscaleInstallParams{Proxy: rpc.ProxyDirect})
	if err == nil || !strings.Contains(err.Error(), "its service did not start") || scm.polls != 120 {
		t.Fatalf("service down: %v (polls %d)", err, scm.polls)
	}

	// msiexec fails: its words come through.
	h.e.TSInstaller.RunMSI = func(context.Context, string, string) (int, error) { return 1618, nil }
	_, err = h.e.TailscaleInstall(ctx, rpc.TailscaleInstallParams{Proxy: rpc.ProxyDirect})
	if !isCode(err, rpc.CodeFailed) || !strings.Contains(err.Error(), "another installation is in progress") {
		t.Fatalf("busy: %v", err)
	}

	// An existing official client is kept: nothing is downloaded or run.
	msiRuns = nil
	h.e.TSInstaller.RunMSI = func(context.Context, string, string) (int, error) { t.Error("ran msiexec"); return 0, nil }
	h.e.TSProbe = &kernel.TailscaleProbe{
		Stat:         func(string) (os.FileInfo, error) { return os.Stat(".") },
		EvalSymlinks: func(p string) (string, error) { return `C:\Program Files\Tailscale\tailscale.exe`, nil },
		Run: func(context.Context, string, ...string) ([]byte, error) {
			return []byte(`{"majorMinorPatch":"1.104.5"}`), nil
		},
	}
	r, err = h.e.TailscaleInstall(ctx, rpc.TailscaleInstallParams{})
	if err != nil || !r.Kept || r.Kind != kernel.TSWindows || r.Version != "1.104.5" || r.UsesSystemVPN {
		t.Fatalf("kept: %+v %v", r, err)
	}
}

// A development helper (not elevated) only plans the MSI install.
func TestTailscaleInstallWindowsDryRun(t *testing.T) {
	h := newHarness(t)
	srv := msiHost(t)
	h.e.Services = &scmFake{}
	h.e.ServiceRunner = &ServiceRunner{DryRun: true}
	h.e.TSProbe = &kernel.TailscaleProbe{Stat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }}
	h.e.TSInstaller = &kernel.TailscaleInstaller{Dir: h.e.Store.Path("tailscale"), PkgsURL: srv.URL, GOOS: "windows", GOARCH: "amd64",
		RunMSI: func(context.Context, string, string) (int, error) { t.Error("ran msiexec"); return 0, nil }}
	r, err := h.e.TailscaleInstall(context.Background(), rpc.TailscaleInstallParams{Proxy: rpc.ProxyDirect})
	if err != nil || r.Service == nil || !r.Service.DryRun || r.Version != "1.104.7" || len(r.Service.Steps) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
}

func zipOf(t *testing.T, name string, body []byte) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(body)
	zw.Close()
	return b.Bytes()
}

// The plain amd64 mihomo does not run on this CPU: kernel.install tries
// the -compatible build of the same release.
func TestKernelInstallCompatibleFallback(t *testing.T) {
	h := newHarness(t)
	v := "v1.19.34"
	assets := map[string][]byte{
		"mihomo-windows-amd64-" + v + ".zip":            zipOf(t, "mihomo-windows-amd64.exe", []byte("v3 build")),
		"mihomo-windows-amd64-compatible-" + v + ".zip": zipOf(t, "mihomo-windows-amd64-compatible.exe", []byte("v1 build")),
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name, ok := strings.CutPrefix(r.URL.Path, "/dl/"); ok {
			w.Write(assets[name])
			return
		}
		var as []map[string]any
		for name, b := range assets {
			s := sha256.Sum256(b)
			as = append(as, map[string]any{"name": name, "browser_download_url": srv.URL + "/dl/" + name, "size": len(b), "digest": "sha256:" + hex.EncodeToString(s[:])})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": v, "assets": as})
	}))
	defer srv.Close()
	var probed []string
	probe := func(_ context.Context, bin string) (string, error) {
		b, _ := os.ReadFile(bin)
		probed = append(probed, string(b))
		if string(b) == "v3 build" {
			return "", errors.New("exit status 0xc000001d") // illegal instruction
		}
		return v, nil
	}
	h.e.Kernels = &kernel.Manager{Dir: h.e.Store.Path("kernels", "mihomo"), API: srv.URL, GOOS: "windows", GOARCH: "amd64",
		HTTP: srv.Client(), X86V3: func() bool { return true }, Probe: probe}
	l, err := h.e.KernelInstall(context.Background(), rpc.KernelInstallParams{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Current != v || len(probed) != 2 || probed[1] != "v1 build" {
		t.Fatalf("list %+v probed %q", l, probed)
	}
	b, _ := os.ReadFile(h.e.Kernels.Binary())
	if string(b) != "v1 build" {
		t.Fatalf("installed %q", b)
	}

	// Neither build runs: the error says so in words.
	h.e.Kernels.Probe = func(context.Context, string) (string, error) { return "", errors.New("exit status 1") }
	h.e.Kernels.Dir = h.e.Store.Path("kernels", "other")
	_, err = h.e.KernelInstall(context.Background(), rpc.KernelInstallParams{})
	if !errors.Is(err, kernel.ErrDoesNotRun) {
		t.Fatalf("err = %v", err)
	}
}
