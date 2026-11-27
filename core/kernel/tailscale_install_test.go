package kernel

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tsArchive is a tgz with fake tailscale/tailscaled scripts under prefix.
func tsArchive(t *testing.T, prefix, version string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	tw := tar.NewWriter(zw)
	for _, name := range []string{"tailscale", "tailscaled", "README"} {
		body := fmt.Sprintf("#!/bin/sh\necho %s\necho '  tailscale commit: synthetic'\n", version)
		tw.WriteHeader(&tar.Header{Name: prefix + name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	// A hostile entry: must not be written anywhere.
	evil := "x"
	tw.WriteHeader(&tar.Header{Name: "../../evil", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	tw.Write([]byte(evil))
	tw.Close()
	zw.Close()
	return b.Bytes()
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type fakeTSHost struct {
	*httptest.Server
	tgz, bottle []byte
	badSum      bool
	sawAuth     string
}

func newFakeTSHost(t *testing.T) *fakeTSHost {
	f := &fakeTSHost{
		tgz:    tsArchive(t, "tailscale_1.104.7_amd64/", "1.104.7"),
		bottle: tsArchive(t, "tailscale/1.104.7/bin/", "1.104.7"),
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stable/":
			json.NewEncoder(w).Encode(map[string]any{
				"TarballsVersion": "1.104.7",
				"Tarballs":        map[string]string{"amd64": "tailscale_1.104.7_amd64.tgz"},
			})
		case "/stable/tailscale_1.104.7_amd64.tgz":
			w.Write(f.tgz)
		case "/stable/tailscale_1.104.7_amd64.tgz.sha256":
			s := sum(f.tgz)
			if f.badSum {
				s = strings.Repeat("0", 64)
			}
			fmt.Fprintln(w, s)
		case "/api/formula/tailscale.json":
			files := map[string]any{}
			for _, tag := range []string{"arm64_sequoia", "arm64_tahoe", "arm64_linux", "x86_64_linux"} {
				files[tag] = map[string]string{"url": f.URL + "/v2/blobs/" + tag, "sha256": sum(f.bottle)}
			}
			json.NewEncoder(w).Encode(map[string]any{
				"versions": map[string]string{"stable": "1.104.7"},
				"bottle":   map[string]any{"stable": map[string]any{"files": files}},
			})
		case "/v2/blobs/arm64_tahoe", "/v2/blobs/arm64_sequoia":
			f.sawAuth = r.Header.Get("Authorization")
			w.Write(f.bottle)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func fakeTSProbe(ctx context.Context, cli string) (string, error) {
	b, err := os.ReadFile(cli)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(b), "\n")
	return strings.TrimPrefix(lines[1], "echo "), nil
}

func TestTailscaleInstallLinux(t *testing.T) {
	f := newFakeTSHost(t)
	dir := t.TempDir()
	in := &TailscaleInstaller{Dir: dir, PkgsURL: f.URL, GOOS: "linux", GOARCH: "amd64", Probe: fakeTSProbe}
	a, err := in.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a.Version != "1.104.7" || a.Source != TSSourcePkgs || a.SHA256 != sum(f.tgz) {
		t.Fatalf("asset %+v", a)
	}
	got, err := in.Install(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if got.Daemon != filepath.Join(dir, "current", "tailscaled") || in.Current() != "1.104.7" {
		t.Fatalf("installed %+v, current %q", got, in.Current())
	}
	for _, n := range []string{"tailscale", "tailscaled"} {
		fi, err := os.Stat(filepath.Join(dir, "1.104.7", n))
		if err != nil || runtime.GOOS != "windows" && fi.Mode().Perm() != 0o755 { // Windows has no mode bits
			t.Fatalf("%s: %v %v", n, fi, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "1.104.7", "README")); err == nil {
		t.Fatal("extracted a file that is not a binary")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(dir)), "evil")); err == nil {
		t.Fatal("path traversal")
	}
}

func TestTailscaleInstallChecksumMismatch(t *testing.T) {
	f := newFakeTSHost(t)
	f.badSum = true
	dir := t.TempDir()
	in := &TailscaleInstaller{Dir: dir, PkgsURL: f.URL, GOOS: "linux", GOARCH: "amd64", Probe: fakeTSProbe}
	a, err := in.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Install(context.Background(), a); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
	if in.Current() != "" {
		t.Fatal("current set after a failed install")
	}
}

func TestTailscaleInstallMacBottle(t *testing.T) {
	f := newFakeTSHost(t)
	dir := t.TempDir()
	in := &TailscaleInstaller{Dir: dir, BrewAPI: f.URL + "/api", GOOS: "darwin", GOARCH: "arm64", MacOSMajor: 27, Probe: fakeTSProbe}
	a, err := in.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// macOS 27 with bottles for 15 and 26 only: the newest older one.
	if a.Name != "arm64_tahoe" || a.Source != TSSourceHomebrew {
		t.Fatalf("asset %+v", a)
	}
	// Bottle URLs must stay on the configured hosts.
	if _, err := in.Install(context.Background(), TailscaleAsset{Version: "1.104.7", Source: TSSourceHomebrew, Name: "x", URL: "https://evil.example.com/x", SHA256: a.SHA256}); err == nil {
		t.Fatal("installed from a foreign host")
	}
	// The fake bottle host is the API host here; allow it.
	got, err := in.Install(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.104.7" || f.sawAuth != "Bearer QQ==" {
		t.Fatalf("installed %+v auth %q", got, f.sawAuth)
	}
}

func TestTailscaleInstallIntelMac(t *testing.T) {
	in := &TailscaleInstaller{Dir: t.TempDir(), BrewAPI: "http://127.0.0.1:1", GOOS: "darwin", GOARCH: "amd64"}
	if _, err := in.Resolve(context.Background()); err == nil || !strings.Contains(err.Error(), "brew install tailscale") {
		t.Fatalf("err = %v", err)
	}
}

func TestPickBottle(t *testing.T) {
	files := map[string]string{"arm64_sequoia": "", "arm64_tahoe": "", "arm64_golden_gate": "", "arm64_linux": "", "x86_64_linux": ""}
	for _, c := range []struct {
		major int
		want  string
	}{{27, "arm64_golden_gate"}, {26, "arm64_tahoe"}, {15, "arm64_sequoia"}, {14, "arm64_golden_gate"}, {0, "arm64_golden_gate"}, {40, "arm64_golden_gate"}} {
		if got := pickBottle(files, c.major); got != c.want {
			t.Errorf("major %d: %q, want %q", c.major, got, c.want)
		}
	}
	files["arm64_future"] = ""
	if got := pickBottle(files, 27); got != "arm64_golden_gate" {
		t.Errorf("unknown newer tag picked for 27: %q", got)
	}
	if got := pickBottle(map[string]string{"x86_64_linux": ""}, 27); got != "" {
		t.Errorf("picked %q", got)
	}
}
