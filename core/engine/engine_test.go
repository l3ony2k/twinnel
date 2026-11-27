package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/engine/enginetest"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

const testSecret = "test-controller-secret"

type fakeTailnet struct {
	mu  sync.Mutex
	raw []byte
	err error
}

func (f *fakeTailnet) Status(context.Context) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.raw, f.err
}

func (f *fakeTailnet) set(raw []byte) {
	f.mu.Lock()
	f.raw = raw
	f.mu.Unlock()
}

type harness struct {
	t       *testing.T
	e       *Engine
	ctrl    *enginetest.Controller
	ctrlSrv *httptest.Server
	tailnet *fakeTailnet
	cfgPath string
	confDir string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake mihomo")
	}
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	bin, err := enginetest.WriteFakeMihomo(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctrl := enginetest.NewController(testSecret)
	srv := httptest.NewServer(ctrl)
	t.Cleanup(srv.Close)
	confDir := filepath.Join(dir, "conf")
	os.MkdirAll(confDir, 0o755)

	_, err = s.UpdateSettings(func(st *store.Settings) error {
		st.Mihomo.Binary = bin
		st.Mihomo.ConfigDir = confDir
		st.Mihomo.ConfigFile = filepath.Join(confDir, "config.yaml")
		st.Mihomo.Controller = srv.Listener.Addr().String()
		st.Tailscale.Coexist = true
		st.LeakGuardGroups = []string{"Google"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sec := &store.MemSecrets{}
	sec.Set(store.SecretController, testSecret)
	status, _ := os.ReadFile("testdata/tailscale-status.json")
	tn := &fakeTailnet{raw: status}

	e := New(s, sec)
	e.Tailnet = tn
	e.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	e.VerifyTimeout = 500 * time.Millisecond
	// The base layer's external-controller would point verification at
	// 127.0.0.1:9090; route every controller address to the fake.
	e.NewController = func(addr, secret string) Controller {
		return &HTTPController{Addr: srv.Listener.Addr().String(), Secret: secret}
	}
	h := &harness{t: t, e: e, ctrl: ctrl, ctrlSrv: srv, tailnet: tn, cfgPath: filepath.Join(confDir, "config.yaml"), confDir: confDir}

	sub, _ := os.ReadFile("testdata/subscription.yaml")
	if _, err := e.PutSubscription(context.Background(), rpc.SubPutParams{ID: "main", Name: "Main", Content: string(sub)}); err != nil {
		t.Fatal(err)
	}
	base, _ := os.ReadFile("testdata/base.yaml")
	if _, err := e.PutLayer(rpc.LayerPutParams{ID: "base", Stage: "base", Content: string(base)}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PutLayer(rpc.LayerPutParams{ID: "my-rules", Content: "+rules:\n  - DOMAIN-SUFFIX,example.org,DIRECT\n"}); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) apply(reason string) *rpc.ApplyResult {
	h.t.Helper()
	r, err := h.e.Apply(context.Background(), rpc.ApplyParams{Reason: reason})
	if err != nil {
		h.t.Fatalf("apply: %v (result %+v)", err, r)
	}
	return r
}

func (h *harness) disk() []byte {
	b, _ := os.ReadFile(h.cfgPath)
	return b
}

func TestApplyPipeline(t *testing.T) {
	h := newHarness(t)
	r := h.apply("first")
	if !r.Valid || !r.Changed || !r.Written || !r.Reloaded || r.HistoryID == "" || r.Error != "" {
		t.Fatalf("result: %+v", r)
	}
	want := []string{"base", "my-rules", CoexistLayerID, LeakGuardLayerID}
	if strings.Join(r.Applied, ",") != strings.Join(want, ",") {
		t.Errorf("applied = %v", r.Applied)
	}
	disk := h.disk()
	for _, s := range []string{"secret: " + testSecret, "DOMAIN-SUFFIX,example.org,DIRECT", "DOMAIN,nas,DIRECT", "mixed-port: 7890"} {
		if !bytes.Contains(disk, []byte(s)) {
			t.Errorf("config lacks %q", s)
		}
	}
	// Coexist rules stay at the very top, above user rules.
	if i, j := bytes.Index(disk, []byte("PROCESS-NAME,tailscaled")), bytes.Index(disk, []byte("example.org")); i < 0 || i > j {
		t.Errorf("tailscale rules not first (%d, %d)", i, j)
	}
	// Leak guard removed DIRECT from Google.
	if bytes.Contains(disk, []byte("proxies: [PROXY, node-a, node-b, DIRECT]")) {
		t.Error("leak guard not applied")
	}
	fi, _ := os.Stat(h.cfgPath)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v", fi.Mode().Perm())
	}
	reloads, loaded, _ := h.ctrl.State()
	if len(reloads) != 1 || reloads[0] != h.cfgPath || !bytes.Equal(loaded, disk) {
		t.Errorf("reloads = %v", reloads)
	}
	// History has the config without the secret.
	hist, data := h.history()
	if len(hist) != 1 || hist[0].Kind != store.HistoryApplied || hist[0].Reason != "first" {
		t.Fatalf("history: %+v", hist)
	}
	if bytes.Contains(data[0], []byte(testSecret)) || bytes.Contains(data[0], []byte("secret:")) {
		t.Error("history contains the controller secret")
	}
	if hist[0].Hash != r.Hash {
		t.Errorf("history hash %s != result hash %s", hist[0].Hash, r.Hash)
	}

	// Same inputs: nothing to do.
	r2 := h.apply("again")
	if r2.Changed || r2.Written || r2.Reloaded || r2.HistoryID != "" {
		t.Errorf("unchanged apply: %+v", r2)
	}
	if reloads, _, _ := h.ctrl.State(); len(reloads) != 1 {
		t.Errorf("reloaded again: %v", reloads)
	}
	// Force reloads.
	r3, err := h.e.Apply(context.Background(), rpc.ApplyParams{Reason: "force", Force: true})
	if err != nil || !r3.Reloaded {
		t.Errorf("forced apply: %+v %v", r3, err)
	}
	// Forcing the same config must not record a backup of our own file.
	hist, _ = h.history()
	for _, e := range hist {
		if e.Kind == store.HistoryBackup {
			t.Errorf("unexpected backup entry %+v", e)
		}
	}
}

func (h *harness) history() ([]store.HistoryEntry, [][]byte) {
	idx, err := h.e.Store.History()
	if err != nil {
		h.t.Fatal(err)
	}
	var data [][]byte
	for _, e := range idx {
		b, _, err := h.e.Store.ReadHistory(e.ID)
		if err != nil {
			h.t.Fatal(err)
		}
		data = append(data, b)
	}
	return idx, data
}

func TestApplyBacksUpForeignFile(t *testing.T) {
	h := newHarness(t)
	handWritten := "# written by hand\nmixed-port: 7890\nsecret: hand-secret\nproxies: []\n"
	os.WriteFile(h.cfgPath, []byte(handWritten), 0o640)
	h.apply("takeover")
	hist, data := h.history()
	if len(hist) != 2 || hist[1].Kind != store.HistoryBackup {
		t.Fatalf("history: %+v", hist)
	}
	if string(data[1]) != "# written by hand\nmixed-port: 7890\nproxies: []\n" {
		t.Errorf("backup = %q", data[1])
	}
	// The file mode of an existing config is kept.
	fi, _ := os.Stat(h.cfgPath)
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
}

func TestApplyFailuresKeepOldConfig(t *testing.T) {
	h := newHarness(t)
	h.apply("good")
	good := h.disk()
	nReloads := func() int { r, _, _ := h.ctrl.State(); return len(r) }

	// 1. mihomo -t rejects: nothing written, no reload.
	h.e.PutLayer(rpc.LayerPutParams{ID: "bad", Content: "proxy-groups+:\n  - {name: " + enginetest.MarkInvalid + ", type: select, proxies: [missing-node]}\n"})
	before := nReloads()
	r, err := h.e.Apply(context.Background(), rpc.ApplyParams{Reason: "invalid"})
	if err == nil || r.Valid || len(r.ValidateErrors) == 0 || r.Written {
		t.Errorf("invalid: %+v %v", r, err)
	}
	if !strings.Contains(r.ValidateErrors[0], "missing-node") {
		t.Errorf("validate errors: %v", r.ValidateErrors)
	}
	if !bytes.Equal(h.disk(), good) || nReloads() != before {
		t.Error("invalid config touched the live config")
	}
	h.e.DeleteLayer("bad")

	// 2. Reload fails: old file restored and reloaded.
	h.e.PutLayer(rpc.LayerPutParams{ID: "reloadfail", Content: "+rules:\n  - DOMAIN," + enginetest.MarkReloadFail + ".example,DIRECT\n"})
	r, err = h.e.Apply(context.Background(), rpc.ApplyParams{Reason: "reload fails"})
	if err == nil || !r.RolledBack || r.Written || r.Reloaded {
		t.Errorf("reload failure: %+v %v", r, err)
	}
	if !bytes.Equal(h.disk(), good) {
		t.Error("old config not restored after reload failure")
	}
	reloads, loaded, _ := h.ctrl.State()
	if len(reloads) < 2 || !bytes.Equal(loaded, good) {
		t.Errorf("old config not reloaded: %d reloads", len(reloads))
	}
	h.e.DeleteLayer("reloadfail")

	// 3. The new config loads but the controller stops answering.
	h.e.PutLayer(rpc.LayerPutParams{ID: "breaks", Content: "+rules:\n  - DOMAIN," + enginetest.MarkBreakController + ".example,DIRECT\n"})
	r, err = h.e.Apply(context.Background(), rpc.ApplyParams{Reason: "verify fails"})
	if err == nil || !r.RolledBack || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("verify failure: %+v %v", r, err)
	}
	if !bytes.Equal(h.disk(), good) {
		t.Error("old config not restored after verify failure")
	}
	_, loaded, down := h.ctrl.State()
	if down || !bytes.Equal(loaded, good) {
		t.Error("fake controller did not get the old config back")
	}
	h.e.DeleteLayer("breaks")

	// History only has the one good apply.
	hist, _ := h.history()
	if len(hist) != 1 {
		t.Errorf("history after failures: %+v", hist)
	}
	// The last result is kept for status.
	st, _ := h.e.Status(context.Background())
	if st.LastApply == nil || st.LastApply.Error == "" {
		t.Errorf("status last apply: %+v", st.LastApply)
	}
}

func TestApplyWhenMihomoStopped(t *testing.T) {
	h := newHarness(t)
	h.ctrl.SetDown(true)
	r := h.apply("offline")
	if !r.Written || r.Reloaded || len(r.Warnings) == 0 || !strings.Contains(r.Warnings[len(r.Warnings)-1], "not running") {
		t.Errorf("offline apply: %+v", r)
	}
	if reloads, _, _ := h.ctrl.State(); len(reloads) != 0 {
		t.Errorf("reloaded while down: %v", reloads)
	}
}

func TestPreviewAndRollback(t *testing.T) {
	h := newHarness(t)
	p, err := h.e.Preview(context.Background())
	if err != nil || !p.Valid || !p.Changed {
		t.Fatalf("preview: %+v %v", p, err)
	}
	if _, err := os.Stat(h.cfgPath); !errors.Is(err, os.ErrNotExist) {
		t.Error("preview wrote the config")
	}
	first := h.apply("v1")
	v1 := h.disk()
	h.e.PutLayer(rpc.LayerPutParams{ID: "more", Content: "+rules:\n  - DOMAIN,more.example,DIRECT\n"})
	h.apply("v2")
	if bytes.Equal(h.disk(), v1) {
		t.Fatal("v2 did not change the config")
	}
	r, err := h.e.Rollback(context.Background(), first.HistoryID)
	if err != nil || !r.Reloaded {
		t.Fatalf("rollback: %+v %v", r, err)
	}
	if !bytes.Equal(h.disk(), v1) {
		t.Error("rollback did not restore v1 (with the secret)")
	}
	hist, _ := h.history()
	if hist[0].Kind != store.HistoryRollback {
		t.Errorf("history: %+v", hist[0])
	}
	if _, err := h.e.Rollback(context.Background(), "20000101T000000Z-deadbeef"); err == nil {
		t.Error("rollback to a missing entry succeeded")
	}
	if _, err := h.e.Rollback(context.Background(), "../settings"); err == nil {
		t.Error("rollback with a bad id succeeded")
	}
}

func TestTailnetRefresh(t *testing.T) {
	h := newHarness(t)
	h.apply("initial")
	ctx := context.Background()

	r, err := h.e.RefreshTailnet(ctx)
	if err != nil || r.Changed || r.Apply != nil {
		t.Errorf("unchanged tailnet: %+v %v", r, err)
	}
	status, _ := os.ReadFile("testdata/tailscale-status.json")
	h.tailnet.set(bytes.Replace(status, []byte(`"nas.example`), []byte(`"nas2.example`), 1))
	r, err = h.e.RefreshTailnet(ctx)
	if err != nil || !r.Changed || r.Apply == nil || !r.Apply.Reloaded {
		t.Fatalf("changed tailnet: %+v %v", r, err)
	}
	if !bytes.Contains(h.disk(), []byte("DOMAIN,nas2,DIRECT")) || bytes.Contains(h.disk(), []byte("DOMAIN,nas,DIRECT")) {
		t.Error("coexist layer not regenerated")
	}
	h.tailnet.mu.Lock()
	h.tailnet.err = errors.New("tailscale not running")
	h.tailnet.mu.Unlock()
	if _, err := h.e.RefreshTailnet(ctx); err == nil {
		t.Error("error not reported")
	}
	st, _ := h.e.Status(ctx)
	if st.Tailnet.Error == "" {
		t.Error("status lacks the tailnet error")
	}
}

func TestSubscriptionRefresh(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sub, _ := os.ReadFile("testdata/subscription.yaml")
	var mu sync.Mutex
	content := sub
	var uas []string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		uas = append(uas, r.UserAgent())
		if r.URL.Query().Get("token") != "s3cr3t-token" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write(content)
	}))
	defer origin.Close()
	var proxied int
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		proxied++
		mu.Unlock()
		// Forward to the origin.
		req, _ := http.NewRequest(http.MethodGet, r.URL.String(), nil)
		req.Header.Set("User-Agent", r.UserAgent())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	defer proxy.Close()
	var usedProxy *url.URL
	h.e.HTTPClient = func(p *url.URL) *http.Client {
		usedProxy = p
		if p == nil {
			return origin.Client()
		}
		pu, _ := url.Parse(proxy.URL)
		return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
	}

	subURL := origin.URL + "/sub?token=s3cr3t-token"
	if _, err := h.e.PutSubscription(ctx, rpc.SubPutParams{ID: "main", URL: subURL}); err != nil {
		t.Fatal(err)
	}
	h.apply("initial")

	// Unchanged content, via the mixed port of the running mihomo.
	r, err := h.e.RefreshSubscription(ctx, "main")
	if err != nil || r.Changed || r.Via != "proxy" || r.Apply != nil {
		t.Fatalf("unchanged: %+v %v", r, err)
	}
	// The mixed port comes from the live config; the factory swaps in the
	// fake proxy.
	if usedProxy == nil || usedProxy.Host != "127.0.0.1:7890" {
		t.Errorf("proxy = %v", usedProxy)
	}
	if proxied != 1 {
		t.Errorf("proxied = %d", proxied)
	}
	if uas[0] != "clash.meta" {
		t.Errorf("user agent %q", uas[0])
	}

	// Changed content triggers apply; mihomo down → direct.
	mu.Lock()
	content = bytes.Replace(sub, []byte("node-b, type: ss, server: 192.0.2.2"), []byte("node-b, type: ss, server: 192.0.2.3"), 1)
	mu.Unlock()
	h.ctrl.SetDown(true)
	r, err = h.e.RefreshSubscription(ctx, "main")
	if err != nil || !r.Changed || r.Via != "direct" || r.Apply == nil || !r.Apply.Written {
		t.Fatalf("changed: %+v %v", r, err)
	}
	if !bytes.Contains(h.disk(), []byte("192.0.2.3")) {
		t.Error("new subscription content not applied")
	}

	// Errors never contain the URL token.
	mu.Lock()
	content = []byte("<html>error page</html>")
	mu.Unlock()
	_, err = h.e.RefreshSubscription(ctx, "main")
	if err == nil || !strings.Contains(err.Error(), "not a") {
		t.Errorf("html content: %v", err)
	}
	h.e.Secrets.Set(store.SubscriptionURLKey("main"), "http://127.0.0.1:1/sub?token=s3cr3t-token")
	_, err = h.e.RefreshSubscription(ctx, "main")
	if err == nil || strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("error leaks the token: %v", err)
	}
	subs, _ := h.e.Subscriptions()
	if subs[0].LastError == "" || strings.Contains(subs[0].LastError, "s3cr3t") || !subs[0].HasURL {
		t.Errorf("sub info: %+v", subs[0])
	}

	// Validation of inputs.
	for _, p := range []rpc.SubPutParams{
		{ID: "x", URL: "file:///etc/passwd"},
		{ID: "x", URL: "ftp://example.org/sub"},
		{ID: "../x", URL: "https://example.org/sub"},
		{ID: "x", Content: "just text"},
	} {
		if _, err := h.e.PutSubscription(ctx, p); err == nil {
			t.Errorf("accepted %+v", p)
		}
	}
	if err := h.e.DeleteSubscription("main"); err == nil {
		t.Error("deleted the active subscription")
	}
}

func TestLayerEdits(t *testing.T) {
	h := newHarness(t)
	for _, p := range []rpc.LayerPutParams{
		{ID: CoexistLayerID, Content: "a: 1\n"},
		{ID: "Bad ID", Content: "a: 1\n"},
		{ID: "x", Content: "a: [1\n"},
		{ID: "x", Stage: "guard", Content: "a: 1\n"},
		{ID: "x", Format: "js", Content: "a: 1\n"},
		{ID: "x", Content: "- just\n- a list\n"},
	} {
		if _, err := h.e.PutLayer(p); err == nil {
			t.Errorf("accepted %+v", p)
		}
	}
	if _, err := h.e.PutLayer(rpc.LayerPutParams{ID: "verge-one", Format: "verge", Content: "prepend-rules:\n  - DOMAIN,v.example,DIRECT\n"}); err != nil {
		t.Fatal(err)
	}
	if err := h.e.ReorderLayers([]string{"my-rules", "base"}); err == nil {
		t.Error("reorder with a missing id accepted")
	}
	if err := h.e.ReorderLayers([]string{"verge-one", "my-rules", "base"}); err != nil {
		t.Fatal(err)
	}
	ls, _ := h.e.Layers()
	var ids []string
	for _, l := range ls {
		ids = append(ids, l.ID)
	}
	if strings.Join(ids, ",") != "verge-one,my-rules,base,"+CoexistLayerID+","+LeakGuardLayerID {
		t.Errorf("order: %v", ids)
	}
	if err := h.e.SetLayerEnabled("my-rules", false); err != nil {
		t.Fatal(err)
	}
	r := h.apply("with verge layer")
	if bytes.Contains(h.disk(), []byte("example.org")) || !bytes.Contains(h.disk(), []byte("DOMAIN,v.example,DIRECT")) {
		t.Error("disabled layer applied or verge layer missing")
	}
	// Base still runs before user layers even though it is listed last.
	if strings.Join(r.Applied, ",") != "base,verge-one,"+CoexistLayerID+","+LeakGuardLayerID {
		t.Errorf("applied: %v", r.Applied)
	}
	lc, err := h.e.Layer(context.Background(), CoexistLayerID)
	if err != nil || !strings.Contains(lc.Content, "DOMAIN,nas,DIRECT") || !lc.Managed {
		t.Errorf("coexist content: %+v %v", lc, err)
	}
	if err := h.e.DeleteLayer(LeakGuardLayerID); err == nil {
		t.Error("deleted a managed layer")
	}
}

func TestSettingsUpdate(t *testing.T) {
	h := newHarness(t)
	off := false
	groups := []string{"Google", "Google", "Meta"}
	s, err := h.e.UpdateSettings(rpc.SettingsUpdate{TailscaleCoexist: &off, LeakGuardGroups: &groups})
	if err != nil || s.TailscaleCoexist || strings.Join(s.LeakGuardGroups, ",") != "Google,Meta" {
		t.Fatalf("%+v %v", s, err)
	}
	bad := 0
	if _, err := h.e.UpdateSettings(rpc.SettingsUpdate{HistoryKeep: &bad}); err == nil {
		t.Error("history_keep 0 accepted")
	}
	r := h.apply("no coexist")
	for _, id := range r.Applied {
		if id == CoexistLayerID {
			t.Error("coexist applied while off")
		}
	}
	if len(r.Warnings) == 0 || !strings.Contains(strings.Join(r.Warnings, "\n"), `"Meta" not found`) {
		t.Errorf("warnings: %v", r.Warnings)
	}
}

func TestEventsPublished(t *testing.T) {
	h := newHarness(t)
	_, next := h.e.Bus.Since(0)
	h.apply("events")
	evs, _ := h.e.Bus.Since(next)
	// The stages, in order, then the outcome.
	var got []string
	for _, ev := range evs {
		if ev.Type == rpc.EventComposeStage {
			var cs rpc.ComposeStage
			json.Unmarshal(ev.Data, &cs)
			got = append(got, cs.Stage)
			continue
		}
		got = append(got, ev.Type)
	}
	want := []string{rpc.StageValidate, rpc.StageWrite, rpc.StageReload, rpc.StageVerify, rpc.EventComposeApplied}
	if !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

func TestSecretHelpers(t *testing.T) {
	cfg := []byte("mixed-port: 7890\nexternal-controller: 127.0.0.1:9090\nmode: rule\n")
	b, err := withSecret(cfg, "abc")
	if err != nil || string(b) != "mixed-port: 7890\nexternal-controller: 127.0.0.1:9090\nsecret: abc\nmode: rule\n" {
		t.Errorf("withSecret: %q %v", b, err)
	}
	back, s, _ := withoutSecret(b)
	if s != "abc" || !bytes.Equal(back, cfg) {
		t.Errorf("withoutSecret: %q %q", back, s)
	}
	if got := stripSecretBytes([]byte("a: 1\nsecret: 'x y'\n  secret: nested\nb: 2")); string(got) != "a: 1\n  secret: nested\nb: 2" {
		t.Errorf("strip: %q", got)
	}
	if mixedPort([]byte("port: 7890\n")) != 7890 || mixedPort([]byte("mixed-port: 7893\nport: 1\n")) != 7893 || mixedPort([]byte("a: 1\n")) != 0 {
		t.Error("mixedPort")
	}
	if controllerAddr("0.0.0.0:9090") != "127.0.0.1:9090" || controllerAddr(":9090") != "127.0.0.1:9090" || controllerAddr("[::]:9090") != "127.0.0.1:9090" {
		t.Error("controllerAddr")
	}
}

// A restarted helper (an upgrade) has not applied anything in this run:
// status.get reports the newest applied history entry as the last apply,
// not none (总览 → 上次应用 said 还没有应用过 after an upgrade).
func TestStatusLastApplyAfterRestart(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if st, _ := h.e.Status(ctx); st.LastApply != nil {
		t.Fatalf("last apply before any: %+v", st.LastApply)
	}
	r := h.apply("")
	again := New(h.e.Store, h.e.Secrets) // the same state dir, a new run
	again.NewController = h.e.NewController
	again.Log = h.e.Log
	st, err := again.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	la := st.LastApply
	if la == nil {
		t.Fatal("no last apply after a restart")
	}
	if d := la.Time.Sub(r.Time); la.HistoryID != r.HistoryID || d < -time.Second || d > 5*time.Second || la.ReasonCode != store.ReasonApply || la.Error != "" {
		t.Fatalf("last apply %+v, want history %s at %v", la, r.HistoryID, r.Time)
	}
}
