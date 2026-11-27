package kernel

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func zipExe(t *testing.T, name, body string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(body))
	zw.Close()
	return b.Bytes()
}

// Windows releases are zips holding mihomo-windows-<arch>.exe; the plain
// amd64 asset is picked, never -compatible, -v1/-v2/-v3 or -goXXX, and it
// lands as <version>\mihomo.exe behind the current link.
func TestWindowsAssetZip(t *testing.T) {
	f := &fakeGitHub{assets: map[string][]byte{}, releases: map[string]string{}, names: map[string][]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	v := "v1.19.34"
	f.addRelease("/repos/MetaCubeX/mihomo/releases/latest", v, map[string][]byte{
		"mihomo-windows-amd64-" + v + ".zip":            zipExe(t, "mihomo-windows-amd64.exe", "exe-amd64"),
		"mihomo-windows-amd64-compatible-" + v + ".zip": []byte("wrong"),
		"mihomo-windows-amd64-v1-go122-" + v + ".zip":   []byte("wrong"),
		"mihomo-windows-arm64-" + v + ".zip":            zipExe(t, "mihomo-windows-arm64.exe", "exe-arm64"),
		"mihomo-linux-amd64-" + v + ".gz":               []byte("wrong"),
	})
	for _, arch := range []string{"amd64", "arm64"} {
		m := &Manager{Dir: filepath.Join(t.TempDir(), "kernels"), API: f.URL, GOOS: "windows", GOARCH: arch, HTTP: f.Client(), X86V3: func() bool { return true },
			Probe: func(_ context.Context, bin string) (string, error) {
				if filepath.Base(bin) != "mihomo.exe" {
					t.Errorf("probed %s", bin)
				}
				return v, nil
			}}
		r, err := m.Check(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		if r.Asset != "mihomo-windows-"+arch+"-"+v+".zip" {
			t.Fatalf("%s: picked %s", arch, r.Asset)
		}
		st, err := m.Install(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		if st.Current != v || filepath.Base(m.Binary()) != "mihomo.exe" {
			t.Fatalf("%+v %s", st, m.Binary())
		}
		if b, err := os.ReadFile(m.Binary()); err != nil || string(b) != "exe-"+arch {
			t.Fatalf("binary %q %v", b, err)
		}
	}
}

func TestUnzipExeRefusesOddArchives(t *testing.T) {
	dir := t.TempDir()
	for name, b := range map[string][]byte{
		"none.zip": zipExe(t, "README.md", "x"),
		"junk.zip": []byte("not a zip"),
	} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, b, 0o600)
		if err := unzipExe(p, filepath.Join(dir, "out.exe")); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, n := range []string{"a.exe", "b.exe"} {
		w, _ := zw.Create(n)
		w.Write([]byte("x"))
	}
	zw.Close()
	p := filepath.Join(dir, "two.zip")
	os.WriteFile(p, b.Bytes(), 0o600)
	if err := unzipExe(p, filepath.Join(dir, "out.exe")); err == nil {
		t.Error("two .exe accepted")
	}
}

func TestTailscaleWindowsKind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"TarballsVersion":"1.104.7","MacZipsVersion":"1.104.6","MSIsVersion":"1.104.8","Version":"1.104.5"}`))
	}))
	defer srv.Close()
	p := &TailscaleProbe{PkgsURL: srv.URL, HTTP: srv.Client(), EvalSymlinks: func(s string) (string, error) { return s, nil }}
	if k := p.Kind(`C:\Program Files\Tailscale\tailscale.exe`); k != TSWindows {
		t.Fatalf("kind %s", k)
	}
	if v, err := p.Latest(context.Background(), TSWindows); err != nil || v != "1.104.8" {
		t.Fatalf("latest %q %v", v, err)
	}
}

// An amd64 CPU without x86-64-v3 gets the -compatible (GOAMD64=v1) build,
// on every OS that publishes one; arm64 is not affected.
func TestCompatibleAssetWithoutV3(t *testing.T) {
	f := &fakeGitHub{assets: map[string][]byte{}, releases: map[string]string{}, names: map[string][]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	v := "v1.19.34"
	f.addRelease("/repos/MetaCubeX/mihomo/releases/latest", v, map[string][]byte{
		"mihomo-windows-amd64-" + v + ".zip":            []byte("v3"),
		"mihomo-windows-amd64-compatible-" + v + ".zip": []byte("v1"),
		"mihomo-windows-arm64-" + v + ".zip":            []byte("arm"),
		"mihomo-linux-amd64-" + v + ".gz":               []byte("v3"),
		"mihomo-linux-amd64-compatible-" + v + ".gz":    []byte("v1"),
		"mihomo-linux-amd64-v1-" + v + ".gz":            []byte("v1 too"),
		"mihomo-darwin-amd64-" + v + ".gz":              []byte("v3"),
		"mihomo-darwin-amd64-compatible-" + v + ".gz":   []byte("v1"),
	})
	for _, c := range []struct {
		goos, arch string
		v3         bool
		want       string
	}{
		{"windows", "amd64", true, "mihomo-windows-amd64-" + v + ".zip"},
		{"windows", "amd64", false, "mihomo-windows-amd64-compatible-" + v + ".zip"},
		{"windows", "arm64", false, "mihomo-windows-arm64-" + v + ".zip"},
		{"linux", "amd64", false, "mihomo-linux-amd64-compatible-" + v + ".gz"},
		{"linux", "amd64", true, "mihomo-linux-amd64-" + v + ".gz"},
		{"darwin", "amd64", false, "mihomo-darwin-amd64-compatible-" + v + ".gz"},
	} {
		v3 := c.v3
		m := &Manager{Dir: t.TempDir(), API: f.URL, GOOS: c.goos, GOARCH: c.arch, HTTP: f.Client(), X86V3: func() bool { return v3 }}
		r, err := m.Check(context.Background(), "")
		if err != nil || r.Asset != c.want || r.Version != v {
			t.Errorf("%s/%s v3=%v: %s %v, want %s", c.goos, c.arch, c.v3, r.Asset, err, c.want)
		}
	}
}

func TestHostX86V3(t *testing.T) {
	// Only a smoke test: off amd64 it is always false.
	if runtime.GOARCH != "amd64" && HostX86V3() {
		t.Error("x86-64-v3 reported on a non-amd64 CPU")
	}
}
