package engine

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/kernel"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/store"
	"github.com/l3ony2k/duet/core/takeover"
)

// asRoot makes the engine believe it runs as root (owner checks on).
func asRoot(t *testing.T) {
	old := geteuid
	geteuid = func() int { return 0 }
	t.Cleanup(func() { geteuid = old })
}

// victim is a file outside the config dir that an attacker wants the root
// helper to overwrite or read.
func victim(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(p, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func checkVictim(t *testing.T, p string) {
	t.Helper()
	if b, err := os.ReadFile(p); err != nil || string(b) != "precious" {
		t.Fatalf("victim file changed: %q %v", b, err)
	}
}

// A symlink planted where the geo download is staged must not be written
// through (it used to be opened with O_TRUNC: an arbitrary root file write
// with content from the config's geox-url).
func TestGeoUpdateIgnoresPlantedSymlink(t *testing.T) {
	h, _ := accessoryHarness(t)
	v := victim(t)
	for _, n := range []string{"GeoIP.dat", "GeoSite.dat", "geoip.metadb", "ASN.mmdb"} {
		if err := os.Symlink(v, filepath.Join(h.confDir, n+kernel.NewSuffix)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.e.GeoUpdate(context.Background(), rpc.AccessoryUpdateParams{}); err != nil {
		t.Fatalf("update: %v", err)
	}
	checkVictim(t, v)
	if b, _ := os.ReadFile(filepath.Join(h.confDir, "GeoIP.dat")); string(b) != "\x0a\x05geoip-v1" {
		t.Errorf("GeoIP.dat %q", b)
	}
}

// The geo file itself being a symlink out of the dir: the swap renames the
// link away instead of writing through it.
func TestGeoUpdateReplacesSymlinkedTarget(t *testing.T) {
	h, _ := accessoryHarness(t)
	v := victim(t)
	os.Symlink(v, filepath.Join(h.confDir, "GeoIP.dat"))
	if _, err := h.e.GeoUpdate(context.Background(), rpc.AccessoryUpdateParams{Force: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	checkVictim(t, v)
	fi, err := os.Lstat(filepath.Join(h.confDir, "GeoIP.dat"))
	if err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("GeoIP.dat is not a regular file: %v %v", fi, err)
	}
}

// A UI dir whose parent is a symlink out of the config dir: nothing is
// extracted or swapped outside.
func TestUIUpdateRefusesSymlinkedDir(t *testing.T) {
	h, _ := accessoryHarness(t)
	h.e.PutLayer(rpc.LayerPutParams{ID: "ui", Content: "external-ui: ui\nexternal-ui-name: zashboard\n"})
	h.apply("ui")
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "keep"), []byte("x"), 0o600)
	if err := os.Symlink(outside, filepath.Join(h.confDir, "ui")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.e.UIUpdate(context.Background(), rpc.AccessoryUpdateParams{Force: true}); err == nil {
		t.Fatal("update through a symlinked ui dir succeeded")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 1 || entries[0].Name() != "keep" {
		t.Errorf("outside dir changed: %v", entries)
	}
}

// config.get live must not read a file the config symlink points to
// outside the config dir (root reading /etc/master.passwd for a client).
func TestConfigGetLiveRefusesSymlinkOut(t *testing.T) {
	h := newHarness(t)
	v := victim(t)
	if err := os.Symlink(v, h.cfgPath); err != nil {
		t.Fatal(err)
	}
	out, err := h.e.ConfigText(context.Background(), rpc.ConfigGetParams{Source: rpc.ConfigLive})
	if err == nil || strings.Contains(out.Content, "precious") {
		t.Fatalf("read through symlink: %q %v", out.Content, err)
	}
	if c, _ := h.e.ControllerCredentials(); strings.Contains(c.Secret, "precious") {
		t.Fatal("credentials read through symlink")
	}
}

// apply with the config file symlinked out of the dir fails and leaves the
// target alone.
func TestApplyDoesNotWriteThroughSymlink(t *testing.T) {
	h := newHarness(t)
	v := victim(t)
	os.Symlink(v, h.cfgPath)
	if _, err := h.e.Apply(context.Background(), rpc.ApplyParams{Reason: "t"}); err == nil {
		t.Error("apply succeeded through a symlinked config")
	}
	checkVictim(t, v)
}

// As root, the config dir must belong to the recorded owner.
func TestOpenConfDirOwner(t *testing.T) {
	asRoot(t)
	dir := t.TempDir()
	if _, err := openConfDir(dir, nil); !errors.Is(err, errConfDir) {
		t.Fatalf("user-owned dir accepted for a root-owned config: %v", err)
	}
	me := os.Getuid()
	d, err := openConfDir(dir, &me)
	if err != nil {
		t.Fatalf("own dir: %v", err)
	}
	d.Close()
	other := me + 1
	if _, err := openConfDir(dir, &other); !errors.Is(err, errConfDir) {
		t.Fatalf("dir of another uid accepted: %v", err)
	}
	// A world-writable dir in the path lets anyone redirect it.
	ww := filepath.Join(dir, "ww")
	os.Mkdir(ww, 0o777)
	os.Chmod(ww, 0o777)
	os.Mkdir(filepath.Join(ww, "conf"), 0o755)
	if _, err := openConfDir(filepath.Join(ww, "conf"), &me); !errors.Is(err, errConfDir) {
		t.Fatalf("world-writable parent accepted: %v", err)
	}
}

// As root, apply into a dir owned by someone other than the recorded uid
// is refused.
func TestApplyAsRootChecksOwner(t *testing.T) {
	h := newHarness(t)
	asRoot(t)
	if _, err := h.e.Apply(context.Background(), rpc.ApplyParams{Reason: "t"}); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("apply into a user dir without config_dir_uid: %v", err)
	}
	me := os.Getuid()
	h.e.Store.UpdateSettings(func(st *store.Settings) error { st.Mihomo.ConfigDirUID = &me; return nil })
	if r, err := h.e.Apply(context.Background(), rpc.ApplyParams{Reason: "t"}); err != nil {
		t.Fatalf("apply with config_dir_uid: %v %+v", err, r)
	}
}

func TestExposureCheck(t *testing.T) {
	st := &store.Settings{}
	st.Mihomo.ConfigDir = "/conf"
	for _, c := range []struct {
		cfg        string
		warn, errs int
	}{
		{"external-controller: 127.0.0.1:9090\n", 0, 0},
		{"external-controller: '[::1]:9090'\n", 0, 0},
		{"external-controller: localhost:9090\n", 0, 0},
		{"external-controller: 0.0.0.0:9090\n", 0, 1},
		{"external-controller: :9090\n", 0, 1},
		{"external-controller: 0.0.0.0:9090\nsecret: s\n", 1, 0},
		{"external-controller-tls: 192.168.1.2:9443\n", 0, 1},
		{"external-controller-unix: /etc/x.sock\n", 0, 1},
		{"external-controller-unix: mihomo.sock\n", 0, 0},
		{"external-controller-unix: ../../etc/x.sock\n", 0, 1},
	} {
		w, e := exposureCheck([]byte(c.cfg), st)
		if len(w) != c.warn || len(e) != c.errs {
			t.Errorf("%q: warnings %v errors %v", c.cfg, w, e)
		}
	}
}

// A layer (or a subscription) that opens the controller to the network
// without a secret is refused by apply.
func TestApplyRefusesOpenController(t *testing.T) {
	h := newHarness(t)
	h.e.Secrets.Delete(store.SecretController)
	// An adopted file without a secret: the helper does not add one.
	os.WriteFile(h.cfgPath, []byte("mixed-port: 7890\n"), 0o644)
	h.e.PutLayer(rpc.LayerPutParams{ID: "open", Content: "external-controller: 0.0.0.0:9090\nsecret: ''\n"})
	before := h.disk()
	r, err := h.e.Apply(context.Background(), rpc.ApplyParams{Reason: "t", Force: true})
	if err == nil {
		t.Fatalf("open controller applied: %+v", r)
	}
	if string(h.disk()) != string(before) {
		t.Error("config written")
	}
}

// As root, service.install refuses a binary a user can replace.
func TestMihomoSpecRefusesUserBinary(t *testing.T) {
	h := newHarness(t)
	asRoot(t)
	st, _ := h.e.Store.Settings()
	if _, err := h.e.mihomoSpec(st); !errors.Is(err, kernel.ErrUntrusted) {
		t.Fatalf("user-owned binary accepted for a root service: %v", err)
	}
}

type homeProbe struct {
	takeover.Probe
	home string
}

func (p homeProbe) UserHome() string { return p.home }

func TestAdoptDirOwner(t *testing.T) {
	asRoot(t)
	home := t.TempDir()
	conf := filepath.Join(home, "conf")
	os.Mkdir(conf, 0o755)
	m := &takeover.MihomoSetup{Dir: conf, Config: filepath.Join(conf, "config.yaml")}
	uid, err := adoptDirOwner(homeProbe{home: home}, m)
	if err != nil || uid == nil || *uid != os.Getuid() {
		t.Fatalf("own dir: %v %v", uid, err)
	}
	// -f in a different place with the same owner is fine.
	other := t.TempDir()
	m.Config = filepath.Join(other, "config.yaml")
	if _, err := adoptDirOwner(homeProbe{home: home}, m); err != nil {
		t.Fatalf("config in another own dir: %v", err)
	}
	// A root-owned dir (/) from a setup without a root LaunchDaemon.
	m = &takeover.MihomoSetup{Dir: "/", Config: "/config.yaml",
		Service: &takeover.ServiceDef{Kind: "launchd-agent", Path: filepath.Join(home, "agent.plist")}}
	if _, err := adoptDirOwner(homeProbe{home: home}, m); err == nil {
		t.Fatal("root-owned dir from a LaunchAgent accepted")
	}
}

func TestNoDowngrade(t *testing.T) {
	mk := func(u string) *http.Request { r, _ := http.NewRequest("GET", u, nil); return r }
	if err := noDowngrade(mk("http://x/b"), []*http.Request{mk("https://x/a")}); err == nil {
		t.Error("https -> http redirect allowed")
	}
	if err := noDowngrade(mk("https://y/b"), []*http.Request{mk("https://x/a")}); err != nil {
		t.Error(err)
	}
	if err := noDowngrade(mk("http://y/b"), []*http.Request{mk("http://x/a")}); err != nil {
		t.Error(err)
	}
}
