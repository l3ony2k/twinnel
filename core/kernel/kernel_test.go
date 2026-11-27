package kernel

import (
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

// fakeBuild is a gzipped shell script that answers -v like mihomo.
func fakeBuild(t *testing.T, version string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	fmt.Fprintf(zw, "#!/bin/sh\n[ \"$1\" = -v ] && echo 'Mihomo Meta %s darwin arm64 with go1.27.4 Mon Nov 24 2026'\n", version)
	zw.Close()
	return b.Bytes()
}

type fakeGitHub struct {
	*httptest.Server
	assets     map[string][]byte   // name → bytes
	releases   map[string]string   // API path → tag
	names      map[string][]string // API path → asset names
	badDigest  bool
	noDigest   bool
	foreignURL bool
	downloads  int
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	f := &fakeGitHub{assets: map[string][]byte{}, releases: map[string]string{}, names: map[string][]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeGitHub) addRelease(apiPath, tag string, assets map[string][]byte) {
	f.releases[apiPath] = tag
	f.names[apiPath] = nil
	for n, b := range assets {
		f.assets[n] = b
		f.names[apiPath] = append(f.names[apiPath], n)
	}
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/dl/") {
		b, ok := f.assets[strings.TrimPrefix(r.URL.Path, "/dl/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		f.downloads++
		w.Write(b)
		return
	}
	tag, ok := f.releases[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	type asset struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Size   int    `json:"size"`
		Digest string `json:"digest,omitempty"`
	}
	var as []asset
	for _, name := range f.names[r.URL.Path] {
		b := f.assets[name]
		sum := sha256.Sum256(b)
		d := "sha256:" + hex.EncodeToString(sum[:])
		if f.badDigest {
			d = "sha256:" + strings.Repeat("0", 64)
		}
		if f.noDigest {
			d = ""
		}
		u := f.URL + "/dl/" + name
		if f.foreignURL {
			u = "https://evil.example/dl/" + name
		}
		as = append(as, asset{name, u, len(b), d})
	}
	json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "published_at": "2026-11-20T00:00:00Z", "assets": as})
}

func newManager(t *testing.T, gh *fakeGitHub, arch string) *Manager {
	return &Manager{Dir: filepath.Join(t.TempDir(), "mihomo"), API: gh.URL, GOOS: "darwin", GOARCH: arch, HTTP: gh.Client(), X86V3: func() bool { return true }}
}

func stableAssets(v string, t *testing.T) map[string][]byte {
	b := fakeBuild(t, v)
	return map[string][]byte{
		"mihomo-darwin-arm64-" + v + ".gz":            b,
		"mihomo-darwin-arm64-go122-" + v + ".gz":      []byte("wrong"),
		"mihomo-darwin-amd64-" + v + ".gz":            b,
		"mihomo-darwin-amd64-v3-" + v + ".gz":         []byte("wrong"),
		"mihomo-darwin-amd64-compatible-" + v + ".gz": []byte("wrong"),
		"mihomo-linux-arm64-" + v + ".gz":             []byte("wrong"),
	}
}

func TestCheckPicksPlatformAsset(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.addRelease("/repos/MetaCubeX/mihomo/releases/latest", "v1.19.34", stableAssets("v1.19.34", t))
	gh.addRelease("/repos/MetaCubeX/mihomo/releases/tags/Prerelease-Alpha", "Prerelease-Alpha", map[string][]byte{
		"mihomo-darwin-arm64-alpha-1a2b3c4.gz":       fakeBuild(t, "alpha-1a2b3c4"),
		"mihomo-darwin-arm64-go120-alpha-1a2b3c4.gz": []byte("wrong"),
	})
	for _, arch := range []string{"arm64", "amd64"} {
		r, err := newManager(t, gh, arch).Check(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		if r.Asset != "mihomo-darwin-"+arch+"-v1.19.34.gz" || r.Version != "v1.19.34" || r.Channel != ChannelStable || !strings.HasPrefix(r.Digest, "sha256:") {
			t.Errorf("%s: %+v", arch, r)
		}
	}
	r, err := newManager(t, gh, "arm64").Check(context.Background(), ChannelAlpha)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "alpha-1a2b3c4" || r.Asset != "mihomo-darwin-arm64-alpha-1a2b3c4.gz" {
		t.Errorf("alpha: %+v", r)
	}
	if _, err := newManager(t, gh, "riscv64").Check(context.Background(), ""); err == nil {
		t.Error("no asset for riscv64 but Check succeeded")
	}
	if _, err := newManager(t, gh, "arm64").Check(context.Background(), "nightly"); err == nil {
		t.Error("unknown channel accepted")
	}
	gh.foreignURL = true
	if _, err := newManager(t, gh, "arm64").Check(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("foreign asset URL: %v", err)
	}
}

func TestInstallSwitchRollback(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.addRelease("/repos/MetaCubeX/mihomo/releases/latest", "v1.19.33", stableAssets("v1.19.33", t))
	m := newManager(t, gh, "arm64")
	ctx := context.Background()

	r, err := m.Check(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.Install(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if st.Current != "v1.19.33" || st.Previous != "" || len(st.Installed) != 1 {
		t.Fatalf("after first install: %+v", st)
	}
	if v, err := ProbeVersion(ctx, m.Binary()); err != nil || v != "v1.19.33" {
		t.Fatalf("current binary: %q %v", v, err)
	}

	gh.addRelease("/repos/MetaCubeX/mihomo/releases/latest", "v1.19.34", stableAssets("v1.19.34", t))
	r, _ = m.Check(ctx, "")
	if !Newer(r.Version, st.Current) {
		t.Errorf("Newer(%s, %s) = false", r.Version, st.Current)
	}
	st, err = m.Install(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if st.Current != "v1.19.34" || st.Previous != "v1.19.33" {
		t.Fatalf("after upgrade: %+v", st)
	}
	if v, _ := ProbeVersion(ctx, m.Binary()); v != "v1.19.34" {
		t.Errorf("binary after upgrade: %s", v)
	}

	st, err = m.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if st.Current != "v1.19.33" || st.Previous != "v1.19.34" {
		t.Fatalf("after rollback: %+v", st)
	}
	if v, _ := ProbeVersion(ctx, m.Binary()); v != "v1.19.33" {
		t.Errorf("binary after rollback: %s", v)
	}

	// Reinstalling the current version is a no-op.
	n := gh.downloads
	r.Version, r.URL, r.Asset = "v1.19.33", gh.URL+"/dl/mihomo-darwin-arm64-v1.19.33.gz", "mihomo-darwin-arm64-v1.19.33.gz"
	sum := sha256.Sum256(gh.assets[r.Asset])
	r.Digest = "sha256:" + hex.EncodeToString(sum[:])
	if st, err = m.Install(ctx, r); err != nil || st.Current != "v1.19.33" {
		t.Errorf("reinstall current: %+v %v", st, err)
	}
	if gh.downloads != n+1 {
		t.Errorf("downloads = %d", gh.downloads)
	}

	// A damaged current binary is replaced by a repair.
	if err := os.WriteFile(m.Binary(), make([]byte, 64), 0o755); err != nil {
		t.Fatal(err)
	}
	m.Repair = true
	if st, err = m.Install(ctx, r); err != nil || st.Current != "v1.19.33" {
		t.Fatalf("repair: %+v %v", st, err)
	}
	if v, err := ProbeVersion(ctx, m.Binary()); err != nil || v != "v1.19.33" {
		t.Fatalf("repaired binary: %q %v", v, err)
	}

	// No stray temp files.
	ents, _ := os.ReadDir(m.Dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}

func TestInstallFailuresKeepCurrent(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.addRelease("/repos/MetaCubeX/mihomo/releases/latest", "v1.19.33", stableAssets("v1.19.33", t))
	m := newManager(t, gh, "arm64")
	ctx := context.Background()
	r, _ := m.Check(ctx, "")
	if _, err := m.Install(ctx, r); err != nil {
		t.Fatal(err)
	}

	gh.addRelease("/repos/MetaCubeX/mihomo/releases/latest", "v1.19.34", stableAssets("v1.19.34", t))
	check := func(name string) {
		t.Helper()
		st, _ := m.List()
		if st.Current != "v1.19.33" || len(st.Installed) != 1 {
			t.Errorf("%s: state changed: %+v", name, st)
		}
	}

	gh.badDigest = true
	r, _ = m.Check(ctx, "")
	if _, err := m.Install(ctx, r); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("bad digest: %v", err)
	}
	check("bad digest")
	gh.badDigest = false

	gh.noDigest = true
	r, _ = m.Check(ctx, "")
	if _, err := m.Install(ctx, r); err == nil || !strings.Contains(err.Error(), "no sha256 digest") {
		t.Errorf("no digest: %v", err)
	}
	check("no digest")
	gh.noDigest = false

	// The binary reports another version.
	gh.assets["mihomo-darwin-arm64-v1.19.34.gz"] = fakeBuild(t, "v1.0.0")
	r, _ = m.Check(ctx, "")
	if _, err := m.Install(ctx, r); err == nil || !strings.Contains(err.Error(), "reports version") {
		t.Errorf("wrong version: %v", err)
	}
	check("wrong version")

	// Not a gzip.
	gh.assets["mihomo-darwin-arm64-v1.19.34.gz"] = []byte("not gzip")
	r, _ = m.Check(ctx, "")
	if _, err := m.Install(ctx, r); err == nil || !strings.Contains(err.Error(), "unpack") {
		t.Errorf("not gzip: %v", err)
	}
	check("not gzip")

	// Bad version strings never become paths.
	r.Version = "../../evil"
	if _, err := m.Install(ctx, r); err == nil {
		t.Error("bad version accepted")
	}
	if _, err := m.Rollback(); err == nil {
		t.Error("rollback without previous succeeded")
	}
}

func TestPrune(t *testing.T) {
	gh := newFakeGitHub(t)
	m := newManager(t, gh, "arm64")
	m.Keep = 3
	ctx := context.Background()
	for _, v := range []string{"v1.19.30", "v1.19.31", "v1.19.32", "v1.19.33", "v1.19.34"} {
		gh.addRelease("/repos/MetaCubeX/mihomo/releases/latest", v, stableAssets(v, t))
		r, _ := m.Check(ctx, "")
		if _, err := m.Install(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := m.List()
	var vs []string
	for _, in := range st.Installed {
		vs = append(vs, in.Version)
	}
	if len(vs) != 3 || st.Current != "v1.19.34" || st.Previous != "v1.19.33" {
		t.Errorf("after prune: %v %+v", vs, st)
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.19.34", "v1.19.33", 1},
		{"1.104.7", "1.104.7", 0},
		{"1.98.1", "1.104.0", -1},
		{"v1.20.0", "v1.19.99", 1},
		{"v1.19.34-rc1", "v1.19.34", 0},
	}
	for _, c := range cases {
		if got := CompareSemver(c.a, c.b); got != c.want {
			t.Errorf("CompareSemver(%s, %s) = %d", c.a, c.b, got)
		}
	}
	if !Newer("alpha-aaaaaaa", "alpha-bbbbbbb") || Newer("alpha-aaaaaaa", "alpha-aaaaaaa") || !Newer("v1.0.0", "") {
		t.Error("Newer")
	}
}

type fakeFI struct{ os.FileInfo }

func TestTailscaleProbe(t *testing.T) {
	pkgs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stable/" || r.URL.Query().Get("mode") != "json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"TarballsVersion":"1.104.7","MacZipsVersion":"1.104.6","Version":"1.104.5"}`))
	}))
	defer pkgs.Close()

	// The macOS and Linux install kinds, on any host: the Unix candidate
	// list (Windows searches Program Files), and the stat keys in the
	// host's separators, as filepath.Join builds them in Kind.
	defer func(c []string) { TailscaleCandidates = c }(TailscaleCandidates)
	TailscaleCandidates = []string{"/opt/homebrew/bin/tailscale", "/usr/local/bin/tailscale", "/Applications/Tailscale.app/Contents/MacOS/Tailscale"}
	files := map[string]bool{}
	links := map[string]string{}
	p := &TailscaleProbe{
		PkgsURL: pkgs.URL, HTTP: pkgs.Client(),
		Stat: func(n string) (os.FileInfo, error) {
			if files[n] {
				return fakeFI{}, nil
			}
			return nil, os.ErrNotExist
		},
		EvalSymlinks: func(n string) (string, error) {
			if l, ok := links[n]; ok {
				return l, nil
			}
			return n, nil
		},
		Run: func(ctx context.Context, bin string, args ...string) ([]byte, error) {
			if strings.Join(args, " ") != "version --json" {
				return nil, fmt.Errorf("unexpected args %v", args)
			}
			return []byte(`{"majorMinorPatch":"1.98.1","short":"1.98.1"}`), nil
		},
	}

	info, err := p.Info(context.Background())
	if err != nil || info.Installed || info.Latest != "1.104.7" {
		t.Errorf("not installed: %+v %v", info, err)
	}

	files["/opt/homebrew/bin/tailscale"] = true
	links["/opt/homebrew/bin/tailscale"] = "/opt/homebrew/Cellar/tailscale/1.98.1/bin/tailscale"
	info, err = p.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Installed || info.Kind != TSBrew || info.Version != "1.98.1" || info.Latest != "1.104.7" || !info.UpdateAvailable {
		t.Errorf("brew: %+v", info)
	}

	app := "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
	if k := p.Kind(app); k != TSStandalone {
		t.Errorf("app kind = %s", k)
	}
	files[filepath.FromSlash("/Applications/Tailscale.app/Contents/_MASReceipt")] = true
	if k := p.Kind(app); k != TSAppStore {
		t.Errorf("app store kind = %s", k)
	}
	files[filepath.FromSlash("/usr/local/sbin/tailscaled")] = true
	if k := p.Kind("/usr/local/sbin/tailscale"); k != TSStatic {
		t.Errorf("static kind = %s", k)
	}
	if k := p.Kind("/somewhere/tailscale"); k != TSUnknown {
		t.Errorf("unknown kind = %s", k)
	}
	if v, _ := p.Latest(context.Background(), TSStandalone); v != "1.104.6" {
		t.Errorf("mac zip version = %s", v)
	}
}

func TestCheckVersion(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.addRelease("/repos/MetaCubeX/mihomo/releases/tags/v1.19.30", "v1.19.30", stableAssets("v1.19.30", t))
	m := newManager(t, gh, "arm64")
	r, err := m.CheckVersion(context.Background(), "v1.19.30")
	if err != nil || r.Version != "v1.19.30" || r.Channel != ChannelStable {
		t.Fatalf("%+v %v", r, err)
	}
	if m.Has("v1.19.30") {
		t.Fatal("Has before install")
	}
	if _, err := m.Install(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if !m.Has("v1.19.30") || m.Has("../x") {
		t.Fatal("Has after install")
	}
	for _, v := range []string{"v1.19.31", "alpha-1a2b3c4", "../../x", ""} {
		if _, err := m.CheckVersion(context.Background(), v); err == nil {
			t.Errorf("%q accepted", v)
		}
	}
}
