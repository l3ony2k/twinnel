package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/engine/enginetest"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/core/tailscale"
	"github.com/l3ony2k/twinnel/core/takeover"
)

// Synthetic hand-made setup: a raw subscription in backup/, a live config
// built from it by hand (header settings, a NAS rule, Tailscale rules
// pointing MagicDNS at the system resolver).

const adoptSubBody = `proxies:
  - {name: n1, type: ss, server: 192.0.2.1, port: 8388, cipher: aes-128-gcm, password: pw-secret-1}
  - {name: n2, type: ss, server: 192.0.2.2, port: 8388, cipher: aes-128-gcm, password: pw-secret-2}
proxy-groups:
  - {name: PROXY, type: select, proxies: [n1, n2, DIRECT]}
rules:
  - DOMAIN-SUFFIX,example.cn,DIRECT
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
`

func adoptSub(ctrl string) string {
	return "port: 7890\nallow-lan: true\nmode: Rule\nexternal-controller: " + ctrl + "\n" + adoptSubBody
}

func adoptLive(ctrl string) string {
	return `# hand-written header
port: 7890
mixed-port: 7899
allow-lan: false
mode: rule
external-controller: ` + ctrl + `
secret: ""
dns:
  enable: true
  enhanced-mode: fake-ip
  nameserver: [https://doh.example/dns-query]
  fake-ip-filter:
    - '+.lan'
    - '+.ts.net'
  nameserver-policy:
    '+.ts.net': [system]
tun:
  enable: true
  auto-detect-interface: true
  route-exclude-address: [192.168.0.0/16, 100.64.0.0/10]
` + adoptSubBody[:strings.Index(adoptSubBody, "rules:")] + `rules:
  # My NAS
  - DOMAIN,nas.example.org,DIRECT
  # Tailscale
  - PROCESS-NAME,tailscaled,DIRECT
  - DOMAIN-SUFFIX,tailscale.com,DIRECT
  - DOMAIN-SUFFIX,ts.net,DIRECT
  - IP-CIDR,100.64.0.0/10,DIRECT,no-resolve
  - DOMAIN-SUFFIX,example.cn,DIRECT
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
`
}

const adoptStatus = `{
  "Version": "1.2.3",
  "BackendState": "Running",
  "MagicDNSSuffix": "tailnet-0000.ts.net",
  "Self": {"DNSName": "alpha.tailnet-0000.ts.net.", "TailscaleIPs": ["100.64.0.1"]},
  "Peer": {"k1": {"DNSName": "beta.tailnet-0000.ts.net.", "TailscaleIPs": ["100.64.0.2"]}}
}`

const adoptSubURL = "https://sub.example/clash/tok-SECRET"

type adoptProbe struct{ dir string }

func (f adoptProbe) Services(context.Context) ([]takeover.ServiceDef, error) {
	return []takeover.ServiceDef{
		{Label: "com.mihomo.proxy", Path: "/L/com.mihomo.proxy.plist", Kind: "launchd-daemon",
			Program: []string{filepath.Join(f.dir, "bin", "mihomo"), "-d", filepath.Join(f.dir, "conf")}},
		{Label: "sh.brew.tailscale", Path: "/L/sh.brew.tailscale.plist", Kind: "launchd-daemon", Program: []string{"/opt/bin/tailscaled"}},
	}, nil
}
func (f adoptProbe) MihomoVersion(context.Context, string, string) (string, error) {
	return "v1.19.34", nil
}
func (f adoptProbe) TailscaleStatus(context.Context) ([]byte, error) { return []byte(adoptStatus), nil }
func (f adoptProbe) TailscaleInterface(context.Context) (string, error) {
	return "utun7", nil
}
func (f adoptProbe) TailscaleInstall() tailscale.Install {
	return tailscale.Install{Kind: tailscale.InstallTailscaled, Source: "brew"}
}
func (f adoptProbe) UserHome() string { return f.dir }

// fakeCheck reports by looking at the config file: worse when bad is set
// and the file is no longer the live one.
type fakeCheck struct {
	id   string
	path string
	live []byte
	bad  *atomic.Bool
}

func (c fakeCheck) ID() string          { return c.id }
func (c fakeCheck) Title() string       { return c.id }
func (c fakeCheck) Platforms() []string { return health.AllPlatforms }
func (c fakeCheck) Run(ctx context.Context, env health.Env) health.Result {
	b, err := os.ReadFile(c.path)
	if err != nil {
		return health.Result{Status: health.StatusSkip, Summary: err.Error()}
	}
	same := bytes.Equal(b, c.live)
	if c.bad.Load() && !same {
		return health.Result{Status: health.StatusFail, Summary: "broken by the new config"}
	}
	if c.id == "tailnet-resolver-binding" && same {
		return health.Result{Status: health.StatusSkip, Summary: "nothing goes to quad-100"}
	}
	return health.Result{Status: health.StatusOK, Summary: "fine"}
}

type platformEnv struct{ health.Env }

func (platformEnv) Platform() string { return runtime.GOOS }

type adoptHarness struct {
	t    *testing.T
	e    *Engine
	ctrl *enginetest.Controller
	dir  string
	live string // path
	data []byte
	bad  *atomic.Bool
	sec  *store.MemSecrets
}

func newAdoptHarness(t *testing.T) *adoptHarness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake mihomo")
	}
	dir := t.TempDir()
	for _, d := range []string{"conf", "backup", "bin"} {
		os.MkdirAll(filepath.Join(dir, d), 0o755)
	}
	if _, err := enginetest.WriteFakeMihomo(filepath.Join(dir, "bin")); err != nil {
		t.Fatal(err)
	}
	ctrl := enginetest.NewController("")
	srv := httptest.NewServer(ctrl)
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	live := filepath.Join(dir, "conf", "config.yaml")
	data := []byte(adoptLive(addr))
	write := func(p, s string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, p), []byte(s), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("conf/config.yaml", string(data), 0o644)
	write("backup/raw-sub.yaml", adoptSub(addr), 0o600)
	write("backup/index.json", `[{"name":"main","source":"`+adoptSubURL+`"}]`, 0o600)

	s, err := store.Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	sec := &store.MemSecrets{}
	e := New(s, sec)
	e.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	e.Tailnet = &fakeTailnet{raw: []byte(adoptStatus)}
	e.TailnetInterface = func(context.Context) (string, error) { return "utun7", nil }
	e.VerifyTimeout = 500 * time.Millisecond
	e.TakeoverProbe = adoptProbe{dir}
	e.AdoptSettle = -1
	e.Services = &fakeManager{}
	e.ServiceRunner = &ServiceRunner{DryRun: true}

	bad := &atomic.Bool{}
	reg := health.NewRegistry()
	for _, id := range AdoptChecks {
		reg.Register(fakeCheck{id: id, path: live, live: data, bad: bad})
	}
	e.SetHealth(&health.Scheduler{Runner: &health.Runner{Registry: reg}, Env: platformEnv{}})
	return &adoptHarness{t: t, e: e, ctrl: ctrl, dir: dir, live: live, data: data, bad: bad, sec: sec}
}

func (h *adoptHarness) plan() rpc.TakeoverPlan {
	h.t.Helper()
	p, err := h.e.TakeoverPlan(context.Background(), rpc.TakeoverPlanParams{})
	if err != nil {
		h.t.Fatal(err)
	}
	if !p.Equivalent || !p.Validated {
		h.t.Fatalf("plan not equivalent/validated:\n%s", p.Summary)
	}
	return p
}

func TestTakeoverPlanStored(t *testing.T) {
	h := newAdoptHarness(t)
	p := h.plan()
	dir, _ := h.e.Store.TakeoverDir(p.PlanID)
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("plan dir %v %v", fi, err)
	}
	for _, f := range []string{"plan.json", "composed.yaml", planStateFile, "summary.txt"} {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", f, fi, err)
		}
	}
	if !p.SubscriptionURL || p.MihomoService != "com.mihomo.proxy" || p.TailscaleSvc != "sh.brew.tailscale" || p.ConfirmHash == "" {
		t.Fatalf("summary %+v", p)
	}
	// No secret in what the client sees.
	all := p.Summary + p.SemDiff + strings.Join(p.Warnings, "")
	for _, s := range []string{"pw-secret", "tok-SECRET"} {
		if strings.Contains(all, s) {
			t.Fatalf("summary leaks %s", s)
		}
	}
	list, err := h.e.TakeoverPlans()
	if err != nil || len(list) != 1 || list[0].PlanID != p.PlanID || list[0].Summary != "" {
		t.Fatalf("plans %+v %v", list, err)
	}
	// Base must be one of the candidates.
	_, err = h.e.TakeoverPlan(context.Background(), rpc.TakeoverPlanParams{Base: "/etc/passwd"})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != rpc.CodeInvalidParams {
		t.Fatalf("arbitrary base: %v", err)
	}
	if _, err := h.e.TakeoverPlan(context.Background(), rpc.TakeoverPlanParams{Base: filepath.Join(h.dir, "backup", "raw-sub.yaml")}); err != nil {
		t.Fatalf("candidate base: %v", err)
	}
	if err := h.e.TakeoverDiscard(rpc.TakeoverDiscardParams{PlanID: p.PlanID}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("discard left %s", dir)
	}
}

func TestAdopt(t *testing.T) {
	h := newAdoptHarness(t)
	p := h.plan()
	ctx := context.Background()

	var re *rpc.Error
	if _, err := h.e.Adopt(ctx, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: "nope"}); !errors.As(err, &re) || re.Code != rpc.CodeInvalidParams {
		t.Fatalf("wrong confirm hash: %v", err)
	}
	r, err := h.e.Adopt(ctx, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash})
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK || r.RolledBack || r.HistoryID == "" || r.BackupHistoryID == "" || r.Apply == nil || !r.Apply.Reloaded {
		t.Fatalf("result %+v", r)
	}
	if len(r.HealthBefore) != len(AdoptChecks) || len(r.HealthAfter) != len(AdoptChecks) {
		t.Fatalf("health %+v / %+v", r.HealthBefore, r.HealthAfter)
	}
	// The new config: coexist with the MagicDNS fix, no secret introduced.
	b, _ := os.ReadFile(h.live)
	for _, want := range []string{"100.100.100.100#utun7", "DOMAIN,nas.example.org,DIRECT", "pw-secret-1"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("applied config lacks %q:\n%s", want, b)
		}
	}
	if s, _ := topLevel(b, "secret"); s != "" {
		t.Fatalf("adopt introduced a secret %q", s)
	}
	if fi, _ := os.Stat(h.live); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode changed to %v", fi.Mode())
	}
	if bk, _ := os.ReadFile(r.Backup); !bytes.Equal(bk, h.data) {
		t.Fatal("exact backup differs from the live config")
	}
	reloads, _, _ := h.ctrl.State()
	if len(reloads) != 1 || reloads[0] != h.live {
		t.Fatalf("reloads %v", reloads)
	}
	// Settings and store.
	st, _ := h.e.Store.Settings()
	if st.Mihomo.ConfigFile != h.live || st.Mihomo.ServiceLabel != "com.mihomo.proxy" || !st.Mihomo.ServiceAdopted ||
		st.Tailscale.ServiceLabel != "sh.brew.tailscale" || !st.Tailscale.Coexist || len(st.LeakGuardGroups) != 0 ||
		st.Adopted == nil || st.Adopted.PlanID != p.PlanID {
		t.Fatalf("settings %+v", st)
	}
	if sub, ok := st.ActiveSubscription(); !ok || sub.ID != AdoptedSubscriptionID || sub.IntervalMinutes != 0 || sub.Name != "main" {
		t.Fatalf("subscription %+v", st.Subscriptions)
	}
	if u, _ := h.sec.Get(store.SubscriptionURLKey(AdoptedSubscriptionID)); u != adoptSubURL {
		t.Fatalf("subscription url %q", u)
	}
	if len(st.Layers) == 0 || st.Layers[0].ID != "base" {
		t.Fatalf("layers %+v", st.Layers)
	}
	// The adopted plist is never replaced.
	if _, err := h.e.ServiceOp(ctx, "mihomo", service.OpInstall); !errors.As(err, &re) {
		t.Fatalf("service install on adopted mihomo: %v", err)
	}
	// Plan state; no second adopt; no discard without force.
	list, _ := h.e.TakeoverPlans()
	if list[0].State != rpc.PlanAdopted || list[0].Backup == "" {
		t.Fatalf("plan state %+v", list[0])
	}
	if _, err := h.e.Adopt(ctx, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash}); err == nil {
		t.Fatal("second adopt accepted")
	}
	if err := h.e.TakeoverDiscard(rpc.TakeoverDiscardParams{PlanID: p.PlanID}); err == nil {
		t.Fatal("discard of the adopted plan accepted")
	}
	// The live config is Twinnel's now: no plan against it (Windows QA
	// round 1, I: 接管 offered one against Twinnel's own config).
	if _, err := h.e.TakeoverPlan(ctx, rpc.TakeoverPlanParams{}); !errors.As(err, &re) || re.Code != rpc.CodeInvalidParams || !strings.Contains(err.Error(), "already managed by Twinnel") {
		t.Fatalf("plan against the adopted setup: %v", err)
	}

	// controller.secure: a new secret, applied; then rotated again.
	sr, err := h.e.SecureController(ctx)
	if err != nil || sr.Replaced || sr.Apply == nil || !sr.Apply.Reloaded {
		t.Fatalf("secure %+v %v", sr, err)
	}
	s1, _ := h.sec.Get(store.SecretController)
	b, _ = os.ReadFile(h.live)
	if fs, _ := topLevel(b, "secret"); s1 == "" || fs != s1 {
		t.Fatalf("secret stored %q, in file %q", s1, fs)
	}
	sr, err = h.e.SecureController(ctx)
	s2, _ := h.sec.Get(store.SecretController)
	if err != nil || !sr.Replaced || s2 == s1 || !sr.Apply.Reloaded {
		t.Fatalf("rotate %+v %v", sr, err)
	}
	if _, _, down := h.ctrl.State(); down {
		t.Fatal("controller down after rotation")
	}
}

func TestAdoptRollsBackWhenHealthWorse(t *testing.T) {
	h := newAdoptHarness(t)
	p := h.plan()
	h.bad.Store(true)
	r, err := h.e.Adopt(context.Background(), rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash})
	if err != nil {
		t.Fatal(err)
	}
	if r.OK || !r.RolledBack || len(r.Worse) == 0 || r.Error == "" {
		t.Fatalf("result %+v", r)
	}
	b, _ := os.ReadFile(h.live)
	if !bytes.Equal(b, h.data) {
		t.Fatal("live config not restored byte for byte")
	}
	reloads, loaded, _ := h.ctrl.State()
	if len(reloads) != 2 || !bytes.Equal(loaded, h.data) {
		t.Fatalf("reloads %v", reloads)
	}
	for _, c := range r.HealthAfterRollback {
		if c.Status == "fail" {
			t.Fatalf("after rollback %+v", r.HealthAfterRollback)
		}
	}
	st, _ := h.e.Store.Settings()
	if len(st.Layers) != 0 || len(st.Subscriptions) != 0 || st.Adopted != nil || st.Tailscale.Coexist || st.Mihomo.ServiceAdopted {
		t.Fatalf("store not reverted: %+v", st)
	}
	if _, err := h.sec.Get(store.SubscriptionURLKey(AdoptedSubscriptionID)); err == nil {
		t.Fatal("subscription url left in secrets")
	}
	if _, err := h.e.Store.ReadLayer("base"); err == nil {
		t.Fatal("layer file left")
	}
	hist, _ := h.e.History()
	if len(hist) == 0 || hist[0].Kind != store.HistoryRollback {
		t.Fatalf("history %+v", hist)
	}
	list, _ := h.e.TakeoverPlans()
	if list[0].State != rpc.PlanRolledBack {
		t.Fatalf("state %s", list[0].State)
	}
	// It can be adopted again once health is fine.
	h.bad.Store(false)
	if r, err := h.e.Adopt(context.Background(), rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash}); err != nil || !r.OK {
		t.Fatalf("re-adopt %+v %v", r, err)
	}
}

func TestAdoptRefusals(t *testing.T) {
	ctx := context.Background()
	var re *rpc.Error

	t.Run("live changed", func(t *testing.T) {
		h := newAdoptHarness(t)
		p := h.plan()
		os.WriteFile(h.live, append(h.data, []byte("# edited\n")...), 0o644)
		_, err := h.e.Adopt(ctx, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash})
		if err == nil || !strings.Contains(err.Error(), "re-plan") {
			t.Fatalf("got %v", err)
		}
		if st, _ := h.e.Store.Settings(); len(st.Layers) != 0 {
			t.Fatal("store changed")
		}
	})
	t.Run("stored secret", func(t *testing.T) {
		h := newAdoptHarness(t)
		p := h.plan()
		h.sec.Set(store.SecretController, "other")
		if _, err := h.e.Adopt(ctx, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash}); !errors.As(err, &re) || re.Code != rpc.CodeInvalidParams {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("not empty", func(t *testing.T) {
		h := newAdoptHarness(t)
		p := h.plan()
		h.e.PutLayer(rpc.LayerPutParams{ID: "mine", Content: "mode: rule\n"})
		if _, err := h.e.Adopt(ctx, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash}); !errors.As(err, &re) || re.Code != rpc.CodeInvalidParams {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("no health", func(t *testing.T) {
		h := newAdoptHarness(t)
		p := h.plan()
		h.e.health.sched = nil
		if _, err := h.e.Adopt(ctx, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash}); !errors.As(err, &re) {
			t.Fatalf("got %v", err)
		}
		r, err := h.e.Adopt(ctx, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash, SkipHealth: true})
		if err != nil || !r.OK {
			t.Fatalf("skip health %+v %v", r, err)
		}
	})
	t.Run("apply fails", func(t *testing.T) {
		h := newAdoptHarness(t)
		p := h.plan()
		h.ctrl.SetDown(false)
		// The reload is refused: Apply restores the file, adopt undoes the import.
		h.e.NewController = func(addr, secret string) Controller { return failReload{&HTTPController{Addr: addr, Secret: secret}} }
		r, err := h.e.Adopt(ctx, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash})
		if err != nil || r.OK || r.Error == "" {
			t.Fatalf("result %+v %v", r, err)
		}
		if b, _ := os.ReadFile(h.live); !bytes.Equal(b, h.data) {
			t.Fatal("live config not restored")
		}
		if st, _ := h.e.Store.Settings(); len(st.Layers) != 0 || st.Adopted != nil {
			t.Fatal("store not reverted")
		}
		list, _ := h.e.TakeoverPlans()
		if list[0].State != rpc.PlanFailed {
			t.Fatalf("state %s", list[0].State)
		}
	})
}

type failReload struct{ *HTTPController }

func (f failReload) Reload(ctx context.Context, path string) error {
	return errors.New("reload refused (test)")
}

func (f failReload) ReloadWith(ctx context.Context, path string, force bool) error {
	return f.Reload(ctx, path)
}
