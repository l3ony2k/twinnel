package engine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/twinnel/core/kernel"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/core/sysdns"
)

type fakeDNS struct{ svcs []sysdns.Service }

func (f *fakeDNS) Services(context.Context) ([]sysdns.Service, error) {
	return slices.Clone(f.svcs), nil
}
func (f *fakeDNS) Set(_ context.Context, name string, servers []string) error {
	for i := range f.svcs {
		if f.svcs[i].Name == name {
			f.svcs[i].Servers = servers
		}
	}
	return nil
}

func TestDNSPointRestore(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if st, err := h.e.DNSStatus(ctx); err != nil || st.Supported {
		t.Fatalf("no platform: %+v %v", st, err)
	}
	if _, err := h.e.DNSPoint(ctx); !isCode(err, rpc.CodeNotImplemented) {
		t.Fatalf("point without platform: %v", err)
	}
	d := &fakeDNS{svcs: []sysdns.Service{{Name: "Wi-Fi"}, {Name: "Ethernet", Servers: []string{"192.0.2.1"}}}}
	h.e.SystemDNS = d
	// mihomo's DNS does not answer: nothing changes.
	h.e.DNSProbe = func(context.Context, string) error { return errors.New("connection refused") }
	if _, err := h.e.DNSPoint(ctx); err == nil || !strings.Contains(err.Error(), "does not answer") {
		t.Fatalf("point while DNS is down: %v", err)
	}
	if d.svcs[0].Servers != nil {
		t.Fatal("changed the DNS while mihomo's does not answer")
	}
	h.e.DNSProbe = func(_ context.Context, addr string) error {
		if addr != "127.0.0.1:53" {
			t.Errorf("probed %s", addr)
		}
		return nil
	}
	st, err := h.e.DNSPoint(ctx)
	if err != nil || !st.Pointed || len(st.Backup) != 2 || len(st.Changed) != 2 {
		t.Fatalf("point: %+v %v", st, err)
	}
	if fi, err := os.Stat(h.e.Store.Path("dns-backup.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup file: %v %v", fi, err)
	}
	st, err = h.e.DNSRestore(ctx)
	if err != nil || st.Backup != nil || !slices.Equal(d.svcs[1].Servers, []string{"192.0.2.1"}) || d.svcs[0].Servers != nil {
		t.Fatalf("restore: %+v %v %+v", st, err, d.svcs)
	}
	if _, err := h.e.DNSRestore(ctx); !isCode(err, rpc.CodeNotFound) {
		t.Fatalf("second restore: %v", err)
	}
}

func isCode(err error, code int) bool {
	var re *rpc.Error
	return errors.As(err, &re) && re.Code == code
}

func tsTarball(version string) []byte {
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	tw := tar.NewWriter(zw)
	for _, n := range []string{"tailscale", "tailscaled"} {
		body := "#!/bin/sh\necho " + version + "\n"
		tw.WriteHeader(&tar.Header{Name: "tailscale_" + version + "_arm64/" + n, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	zw.Close()
	return b.Bytes()
}

func TestTailscaleInstall(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tgz := tsTarball("1.104.7")
	sum := sha256.Sum256(tgz)
	var downloads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stable/":
			json.NewEncoder(w).Encode(map[string]any{"Tarballs": map[string]string{"arm64": "tailscale_1.104.7_arm64.tgz"}})
		case "/stable/tailscale_1.104.7_arm64.tgz":
			downloads++
			w.Write(tgz)
		case "/stable/tailscale_1.104.7_arm64.tgz.sha256":
			fmt.Fprintln(w, hex.EncodeToString(sum[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	m := &fakeManager{}
	ex, files := &recExec{}, &recFiles{}
	h.e.Services = m
	h.e.ServiceRunner = &ServiceRunner{Exec: ex, Files: files}
	h.e.TSInstaller = &kernel.TailscaleInstaller{Dir: h.e.Store.Path("tailscale"), PkgsURL: srv.URL, GOOS: "linux", GOARCH: "arm64",
		Probe: func(context.Context, string) (string, error) { return "1.104.7", nil }}
	h.e.TSProbe = &kernel.TailscaleProbe{Stat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }}

	r, err := h.e.TailscaleInstall(ctx, rpc.TailscaleInstallParams{Proxy: rpc.ProxyDirect})
	if err != nil {
		t.Fatal(err)
	}
	cli, daemon := h.e.TSInstaller.Binaries()
	if r.Kept || r.Version != "1.104.7" || r.Daemon != daemon || r.Via != "direct" || r.Service == nil || r.Service.DryRun {
		t.Fatalf("result %+v", r)
	}
	spec := m.specs[len(m.specs)-1]
	if spec.Name != "tailscaled" || spec.Program != daemon || spec.Label != "com.tailscale.tailscaled" {
		t.Fatalf("service spec %+v", spec)
	}
	st, _ := h.e.Store.Settings()
	if st.Tailscale.Binary != cli || st.Tailscale.DaemonBinary != daemon {
		t.Fatalf("settings %+v", st.Tailscale)
	}
	// Again: nothing is downloaded, the service is made sure of.
	r, err = h.e.TailscaleInstall(ctx, rpc.TailscaleInstallParams{})
	if err != nil || !r.Kept || r.Kind != "twinnel" || downloads != 1 {
		t.Fatalf("second run %+v %v (downloads %d)", r, err, downloads)
	}
	// An install Twinnel did not make is kept, and nothing else happens.
	n := len(m.specs)
	h.e.TSProbe = &kernel.TailscaleProbe{
		Stat: func(p string) (os.FileInfo, error) {
			if p == "/Applications/Tailscale.app/Contents/MacOS/Tailscale" {
				return os.Stat(".")
			}
			return nil, os.ErrNotExist
		},
		EvalSymlinks: func(p string) (string, error) { return p, nil },
		Run: func(context.Context, string, ...string) ([]byte, error) {
			return []byte(`{"majorMinorPatch":"1.104.7"}`), nil
		},
	}
	r, err = h.e.TailscaleInstall(ctx, rpc.TailscaleInstallParams{})
	if err != nil || !r.Kept || !r.UsesSystemVPN || r.Kind != kernel.TSStandalone || len(m.specs) != n {
		t.Fatalf("existing app: %+v %v", r, err)
	}
}

func TestDownloadProxyChoices(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	st, _ := h.e.Store.Settings()
	for _, bad := range []string{"ftp://x", "file:///etc/passwd", "nonsense"} {
		if _, _, _, err := h.e.downloadVia(ctx, st, bad); err == nil {
			t.Errorf("accepted proxy %q", bad)
		}
	}
	for _, good := range []string{"http://192.0.2.10:7890", "socks5://127.0.0.1:1080"} {
		if _, via, _, err := h.e.downloadVia(ctx, st, good); err != nil || via != "url" {
			t.Errorf("%s: %s %v", good, via, err)
		}
	}
	// The subscription's proxy: a temporary mihomo with the composed
	// config, stopped after the download.
	var cfg []byte
	stopped := false
	h.e.TempProxy = func(_ context.Context, bin string, c []byte, port int) (func(), error) {
		if bin != st.Mihomo.Binary {
			t.Errorf("binary %s", bin)
		}
		cfg = c
		return func() { stopped = true }, nil
	}
	_, via, stop, err := h.e.downloadVia(ctx, st, rpc.ProxySubscription)
	if err != nil || via != "subscription" {
		t.Fatalf("subscription proxy: %v", err)
	}
	stop()
	if !stopped {
		t.Fatal("not stopped")
	}
	var got map[string]any
	if err := yaml.Unmarshal(cfg, &got); err != nil {
		t.Fatal(err)
	}
	if got["tun"].(map[string]any)["enable"] != false || got["dns"].(map[string]any)["enable"] != false ||
		got["external-controller"] != "" || got["bind-address"] != "127.0.0.1" || got["allow-lan"] != false {
		t.Fatalf("temp config %v", got)
	}
	rules := got["rules"].([]any)
	if len(rules) != 1 || !strings.HasPrefix(rules[0].(string), "MATCH,") {
		t.Fatalf("rules %v", rules)
	}
	if _, ok := got["proxies"]; !ok {
		t.Fatal("proxies dropped")
	}
	// No binary at all: a clear error.
	h.e.Store.UpdateSettings(func(s *store.Settings) error { s.Mihomo.Binary = ""; return nil })
	st, _ = h.e.Store.Settings()
	if _, _, _, err := h.e.downloadVia(ctx, st, rpc.ProxySubscription); err == nil || !strings.Contains(err.Error(), "no mihomo binary") {
		t.Fatalf("no binary: %v", err)
	}
}

func TestTempProxyConfigNeedsAProxy(t *testing.T) {
	if _, err := TempProxyConfig([]byte("mixed-port: 7890\n"), 1234); !errors.Is(err, errNoProxy) {
		t.Fatalf("err = %v", err)
	}
	out, err := TempProxyConfig([]byte("proxies:\n  - {name: a, type: http, server: 192.0.2.1, port: 80}\n"), 1234)
	if err != nil || !strings.Contains(string(out), "MATCH,a") || !strings.Contains(string(out), "mixed-port: 1234") {
		t.Fatalf("%s %v", out, err)
	}
}
