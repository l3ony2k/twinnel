package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/l3ony2k/duet/core/engine/enginetest"
	"github.com/l3ony2k/duet/core/kernel"
	"github.com/l3ony2k/duet/core/rpc"
)

// fakeGitHub serves release metadata and asset downloads for
// meta-rules-dat and zashboard. Every request to any host is routed to it.
type fakeGitHub struct {
	mu      sync.Mutex
	files   map[string][]byte // asset name -> content served
	digests map[string][]byte // asset name -> content the digest is computed from
	tag     string
	srv     *httptest.Server
	apiHits int
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	g := &fakeGitHub{files: map[string][]byte{}, digests: map[string][]byte{}, tag: "v1"}
	g.set("geoip.dat", []byte("\x0a\x05geoip-v1"))
	g.set("geosite.dat", []byte("\x0a\x05geosite-v1"))
	g.set("geoip.metadb", []byte("mmdb-v1\xab\xcd\xefMaxMind.com"))
	g.set("GeoLite2-ASN.mmdb", []byte("asn-v1\xab\xcd\xefMaxMind.com"))
	g.set("dist.zip", uiZip(t, "v1"))
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGitHub) set(name string, b []byte) {
	g.mu.Lock()
	g.files[name], g.digests[name] = b, b
	g.mu.Unlock()
}

func (g *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	type asset struct {
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
	}
	release := func(names ...string) {
		var as []asset
		for _, n := range names {
			sum := sha256.Sum256(g.digests[n])
			as = append(as, asset{n, int64(len(g.digests[n])), "sha256:" + hex.EncodeToString(sum[:])})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": g.tag, "assets": as})
	}
	if strings.HasPrefix(r.URL.Path, "/repos/") {
		g.apiHits++
	}
	switch p := r.URL.Path; {
	case p == "/repos/MetaCubeX/meta-rules-dat/releases/tags/latest":
		release("geoip.dat", "geosite.dat", "geoip.metadb", "GeoLite2-ASN.mmdb")
	case p == "/repos/Zephyruso/zashboard/releases/latest":
		release("dist.zip")
	case strings.HasPrefix(p, "/MetaCubeX/meta-rules-dat/releases/download/latest/"),
		strings.HasPrefix(p, "/Zephyruso/zashboard/releases/latest/download/"):
		b, ok := g.files[filepath.Base(p)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	default:
		http.NotFound(w, r)
	}
}

type rewrite struct{ host string }

func (rw rewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = "http", rw.host
	return http.DefaultTransport.RoundTrip(r)
}

func uiZip(t *testing.T, version string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"dist/index.html":     "<html>" + version + "</html>",
		"dist/assets/app.js":  "console.log('" + version + "')",
		"dist/assets/app.css": "body{}",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func accessoryHarness(t *testing.T) (*harness, *fakeGitHub) {
	h := newHarness(t)
	g := newFakeGitHub(t)
	host := g.srv.Listener.Addr().String()
	h.e.HTTPClient = func(*url.URL) *http.Client { return &http.Client{Transport: rewrite{host}} }
	return h, g
}

func TestGeoCheckAndUpdate(t *testing.T) {
	h, g := accessoryHarness(t)
	ctx := context.Background()
	c, err := h.e.GeoCheck(ctx)
	if err != nil || len(c.Files) != 4 {
		t.Fatalf("check: %+v %v", c, err)
	}
	if g.apiHits != 1 {
		t.Errorf("%d API requests for one check, want 1", g.apiHits)
	}
	for _, f := range c.Files {
		if f.Installed || !f.UpdateAvailable || !f.Verifiable || f.Error != "" {
			t.Errorf("fresh %+v", f)
		}
	}
	if c.Files[2].File != filepath.Join(h.confDir, "geoip.metadb") || c.Files[3].File != filepath.Join(h.confDir, "ASN.mmdb") {
		t.Errorf("names: %s %s", c.Files[2].File, c.Files[3].File)
	}

	r, err := h.e.GeoUpdate(ctx, rpc.AccessoryUpdateParams{})
	if err != nil || len(r.Updated) != 4 || !r.ReloadNeeded {
		t.Fatalf("update: %+v %v", r, err)
	}
	if b, _ := os.ReadFile(filepath.Join(h.confDir, "GeoSite.dat")); string(b) != "\x0a\x05geosite-v1" {
		t.Errorf("geosite %q", b)
	}
	r, err = h.e.GeoUpdate(ctx, rpc.AccessoryUpdateParams{})
	if err != nil || len(r.Updated) != 0 || len(r.Skipped) != 4 {
		t.Fatalf("second update: %+v %v", r, err)
	}

	// A new release where geosite's download doesn't match its digest:
	// nothing changes, not even geoip which downloaded fine.
	g.set("geoip.dat", []byte("\x0a\x05geoip-v2"))
	g.set("geosite.dat", []byte("\x0a\x05geosite-v2"))
	g.mu.Lock()
	g.files["geosite.dat"] = []byte("\x0a\x05tampered!!")
	g.mu.Unlock()
	if r, err := h.e.GeoUpdate(ctx, rpc.AccessoryUpdateParams{}); err == nil || !strings.Contains(r.Error, "does not match") {
		t.Fatalf("tampered: %+v %v", r, err)
	}
	if b, _ := os.ReadFile(filepath.Join(h.confDir, "GeoIP.dat")); string(b) != "\x0a\x05geoip-v1" {
		t.Errorf("geoip changed after a failed set: %q", b)
	}
	if m, _ := filepath.Glob(filepath.Join(h.confDir, "*"+kernel.NewSuffix)); len(m) != 0 {
		t.Errorf("leftovers %v", m)
	}

	// An HTML error page with a matching digest still fails the format check.
	g.set("geosite.dat", []byte("<html>rate limited</html>"))
	if _, err := h.e.GeoUpdate(ctx, rpc.AccessoryUpdateParams{}); err == nil || !strings.Contains(err.Error(), "not a geodata") {
		t.Fatalf("html: %v", err)
	}

	// mihomo -t fails with the new files: everything is put back.
	g.set("geosite.dat", []byte("\x0a\x05geosite-v2"))
	h.apply("config on disk")
	cfg, _ := os.ReadFile(h.cfgPath)
	os.WriteFile(h.cfgPath, append(cfg, []byte("# "+enginetest.MarkInvalid+"\n")...), 0o600)
	r, err = h.e.GeoUpdate(ctx, rpc.AccessoryUpdateParams{})
	if err == nil || !r.RolledBack {
		t.Fatalf("validate failure: %+v %v", r, err)
	}
	if b, _ := os.ReadFile(filepath.Join(h.confDir, "GeoIP.dat")); string(b) != "\x0a\x05geoip-v1" {
		t.Errorf("geoip not restored: %q", b)
	}
	os.WriteFile(h.cfgPath, cfg, 0o600)
	if r, err = h.e.GeoUpdate(ctx, rpc.AccessoryUpdateParams{}); err != nil || len(r.Updated) != 2 {
		t.Fatalf("v2: %+v %v", r, err)
	}
	if b, _ := os.ReadFile(filepath.Join(h.confDir, "GeoIP.dat"+kernel.BackupSuffix)); string(b) != "\x0a\x05geoip-v1" {
		t.Errorf("backup %q", b)
	}
}

func TestGeoCustomURLsAndExistingNames(t *testing.T) {
	h, _ := accessoryHarness(t)
	os.WriteFile(filepath.Join(h.confDir, "Country.mmdb"), []byte("old"), 0o644)
	h.e.PutLayer(rpc.LayerPutParams{ID: "geo", Content: "geox-url:\n  geosite: https://example.com/custom/geosite.dat?token=abc\n"})
	h.apply("geox-url")
	c, err := h.e.GeoCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Files[2].File != filepath.Join(h.confDir, "Country.mmdb") || !c.Files[2].Installed {
		t.Errorf("mmdb %+v", c.Files[2])
	}
	gs := c.Files[1]
	if gs.URL != "https://example.com/custom/geosite.dat?…" || gs.Verifiable {
		t.Errorf("custom geosite %+v", gs)
	}
}

func TestRateLimitMessage(t *testing.T) {
	h, _ := accessoryHarness(t)
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1795680000")
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer limited.Close()
	h.e.HTTPClient = func(*url.URL) *http.Client { return &http.Client{Transport: rewrite{limited.Listener.Addr().String()}} }
	c, _ := h.e.GeoCheck(context.Background())
	if !strings.Contains(c.Files[0].Error, "rate limit") || c.Files[0].UpdateAvailable {
		t.Errorf("%+v", c.Files[0])
	}
}

func TestUIUpdate(t *testing.T) {
	h, g := accessoryHarness(t)
	ctx := context.Background()
	if c, _ := h.e.UICheck(ctx); c.Files[0].Error == "" {
		t.Errorf("no config: %+v", c)
	}
	h.e.PutLayer(rpc.LayerPutParams{ID: "ui", Content: "external-ui: ui\nexternal-ui-name: zashboard\n"})
	h.apply("ui")
	c, err := h.e.UICheck(ctx)
	f := c.Files[0]
	if err != nil || f.Installed || !f.UpdateAvailable || f.Remote != "v1" || f.File != filepath.Join(h.confDir, "ui", "zashboard") {
		t.Fatalf("check: %+v %v", f, err)
	}
	r, err := h.e.UIUpdate(ctx, rpc.AccessoryUpdateParams{})
	if err != nil || len(r.Updated) != 1 || r.Files[0].UpdateAvailable || r.Files[0].SHA256 != "v1" {
		t.Fatalf("update: %+v %v", r, err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.File, "index.html")); string(b) != "<html>v1</html>" {
		t.Errorf("index %q", b)
	}
	if _, err := os.Stat(filepath.Join(f.File, "assets", "app.js")); err != nil {
		t.Error(err)
	}

	g.mu.Lock()
	g.tag = "v2"
	g.mu.Unlock()
	g.set("dist.zip", uiZip(t, "v2"))
	if r, err = h.e.UIUpdate(ctx, rpc.AccessoryUpdateParams{}); err != nil || r.Files[0].SHA256 != "v2" {
		t.Fatalf("v2: %+v %v", r, err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.File+kernel.BackupSuffix, "index.html")); string(b) != "<html>v1</html>" {
		t.Errorf("backup %q", b)
	}

	// A zip escaping the dir is refused and the installed UI stays.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../../evil.html")
	w.Write([]byte("x"))
	zw.Close()
	g.mu.Lock()
	g.tag = "v3"
	g.mu.Unlock()
	g.set("dist.zip", buf.Bytes())
	if _, err := h.e.UIUpdate(ctx, rpc.AccessoryUpdateParams{}); err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("zip slip: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.File, "index.html")); string(b) != "<html>v2</html>" {
		t.Errorf("index after refused update %q", b)
	}

	// external-ui outside the config dir is refused.
	h.e.PutLayer(rpc.LayerPutParams{ID: "ui", Content: "external-ui: /tmp\n"})
	h.apply("bad ui")
	if c, _ := h.e.UICheck(ctx); !strings.Contains(c.Files[0].Error, "outside the config dir") {
		t.Errorf("outside: %+v", c.Files[0])
	}
}
