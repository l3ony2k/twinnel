package setup

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// fakeHelper answers the helper methods a setup uses, in memory.
type fakeHelper struct {
	calls     []string
	subs      []rpc.Subscription
	urls      map[string]string
	content   map[string]string
	layers    []rpc.Layer
	layerText map[string]string
	settings  rpc.Settings
	kernel    string
	kernelErr error
	proxies   []string // the Proxy of each kernel.install
	applied   bool
	svc       rpc.ServiceStatus
	dryRun    bool
	dns       rpc.DNSStatus
	tsInst    bool
	tsErr     error
	health    []health.CheckReport
}

func newFakeHelper() *fakeHelper {
	return &fakeHelper{urls: map[string]string{}, content: map[string]string{}, layerText: map[string]string{},
		dns:    rpc.DNSStatus{Supported: true, Target: []string{"127.0.0.1"}},
		health: []health.CheckReport{{ID: "magicdns", Title: "MagicDNS", Result: health.Result{Status: health.StatusOK}}}}
}

func (h *fakeHelper) Call(_ context.Context, method string, params, result any) error {
	h.calls = append(h.calls, method)
	out := func(v any) error {
		if result == nil {
			return nil
		}
		b, _ := json.Marshal(v)
		return json.Unmarshal(b, result)
	}
	switch method {
	case rpc.MethodSubList:
		return out(h.subs)
	case rpc.MethodSubPut:
		p := params.(rpc.SubPutParams)
		if p.URL != "" {
			h.urls[p.ID] = p.URL
		}
		if p.Content != "" {
			h.content[p.ID] = p.Content
		}
		i := slices.IndexFunc(h.subs, func(s rpc.Subscription) bool { return s.ID == p.ID })
		if i < 0 {
			h.subs = append(h.subs, rpc.Subscription{ID: p.ID, Name: p.Name, Active: len(h.subs) == 0})
			i = len(h.subs) - 1
		}
		if p.Content != "" {
			h.subs[i].Hash = "h"
		}
		return out(h.subs[i])
	case rpc.MethodSubRefresh:
		id := params.(rpc.IDParams).ID
		if h.urls[id] == "" {
			return &rpc.Error{Code: rpc.CodeFailed, Message: "no URL"}
		}
		for i := range h.subs {
			if h.subs[i].ID == id {
				h.subs[i].Hash = "h"
			}
		}
		return out(rpc.SubRefreshResult{Changed: true, Via: "direct"})
	case rpc.MethodSubActivate:
		id := params.(rpc.IDParams).ID
		for i := range h.subs {
			h.subs[i].Active = h.subs[i].ID == id
		}
		return nil
	case rpc.MethodKernelList:
		return out(rpc.KernelList{Current: h.kernel})
	case rpc.MethodKernelInstall:
		p := params.(rpc.KernelInstallParams)
		h.proxies = append(h.proxies, p.Proxy)
		if h.kernelErr != nil {
			return h.kernelErr
		}
		h.kernel = "v1.19.34"
		return out(rpc.KernelList{Current: h.kernel})
	case rpc.MethodGeoUpdate:
		return errors.New("geo: offline")
	case rpc.MethodLayerList:
		return out(h.layers)
	case rpc.MethodLayerPut:
		p := params.(rpc.LayerPutParams)
		h.layerText[p.ID] = p.Content
		l := rpc.Layer{ID: p.ID, Name: p.Name, Stage: p.Stage, Enabled: p.Enabled == nil || *p.Enabled}
		if l.Stage == "" {
			l.Stage = "user"
		}
		if i := slices.IndexFunc(h.layers, func(x rpc.Layer) bool { return x.ID == p.ID }); i >= 0 {
			h.layers[i] = l
		} else {
			h.layers = append(h.layers, l)
		}
		return out(l)
	case rpc.MethodLayerReorder:
		ids := params.(rpc.ReorderParams).IDs
		var re []rpc.Layer
		for _, id := range ids {
			i := slices.IndexFunc(h.layers, func(x rpc.Layer) bool { return x.ID == id })
			re = append(re, h.layers[i])
		}
		h.layers = re
		return nil
	case rpc.MethodSettingsGet:
		return out(h.settings)
	case rpc.MethodSettingsUpdate:
		u := params.(rpc.SettingsUpdate)
		if u.TailscaleCoexist != nil {
			h.settings.TailscaleCoexist = *u.TailscaleCoexist
		}
		if u.LeakGuardGroups != nil {
			h.settings.LeakGuardGroups = *u.LeakGuardGroups
		}
		return out(h.settings)
	case rpc.MethodComposePreview:
		return out(rpc.ComposeResult{Valid: true, Applied: []string{"base", "tailscale-coexist", "leak-guard"}})
	case rpc.MethodComposeApply:
		if params.(rpc.ApplyParams).ReasonCode != "setup" {
			return errors.New("reason code")
		}
		h.applied = true
		return out(rpc.ApplyResult{Written: true})
	case rpc.MethodStatus:
		st := rpc.Status{}
		st.Mihomo.Reachable = h.svc.Running
		if h.applied {
			st.LastApply = &rpc.ApplyResult{}
		}
		return out(st)
	case rpc.MethodServiceStatus:
		return out(h.svc)
	case rpc.MethodServiceInstall:
		if !h.dryRun {
			h.svc = rpc.ServiceStatus{Installed: true, Running: true}
		}
		return out(rpc.ServiceOpResult{DryRun: h.dryRun})
	case rpc.MethodDNSStatus:
		return out(h.dns)
	case rpc.MethodDNSPoint:
		h.dns.Pointed = true
		return out(h.dns)
	case rpc.MethodTailscaleInfo:
		return out(rpc.TailscaleInfo{Installed: h.tsInst})
	case rpc.MethodTailscaleInstall:
		if h.tsErr != nil {
			return h.tsErr
		}
		h.tsInst = true
		return out(rpc.TailscaleInstallResult{Version: "1.104.7"})
	case rpc.MethodTailnetRefresh:
		return nil
	case rpc.MethodHealthRun:
		b, _ := json.Marshal(health.Report{Checks: h.health})
		return out(rpc.HealthReport{Report: b})
	}
	return errors.New("unexpected method " + method)
}

type fakeTailnet struct {
	backend string
	polls   int
	started bool
}

func (t *fakeTailnet) LoginState(context.Context) (LoginState, error) {
	t.polls++
	if t.started && t.polls > 3 {
		t.backend = "Running"
	}
	ls := LoginState{Backend: t.backend, Tailnet: "example.ts.net"}
	if t.started && t.backend != "Running" {
		ls.AuthURL = "https://login.tailscale.com/a/synthetic"
	}
	return ls, nil
}

func (t *fakeTailnet) StartLogin(context.Context) error { t.started = true; return nil }

func newTestFlow(h *fakeHelper, installed *bool, o Options) (*Flow, *fakeTailnet, *[]string) {
	tn := &fakeTailnet{backend: "NeedsLogin"}
	var opened []string
	env := Env{
		Connect: func(context.Context) (Caller, error) {
			if !*installed {
				return nil, errors.New("no socket")
			}
			return h, nil
		},
		InstallHelper: func(context.Context) error { *installed = true; return nil },
		Tailnet:       tn,
		OpenURL:       func(u string) error { opened = append(opened, u); return nil },
		Sleep:         func(context.Context, time.Duration) error { return nil },
	}
	return New(env, o), tn, &opened
}

func TestFlowFromNothing(t *testing.T) {
	h := newFakeHelper()
	installed := false
	f, _, opened := newTestFlow(h, &installed, Options{})
	f.Detect(context.Background())
	if f.Step(StepHelper).State != Pending {
		t.Fatal("helper detected without one")
	}
	// No subscription yet: the flow waits for one.
	err := f.Run(context.Background())
	var se *StepError
	if !errors.As(err, &se) || se.Step != StepSubscription || se.Code != CodeNeedsSubscription {
		t.Fatalf("err = %v", err)
	}
	if !installed || f.Step(StepHelper).State != Done || f.Step(StepSubscription).State != Waiting {
		t.Fatalf("steps %+v", f.Steps())
	}
	f.Opts.SubscriptionURL = "https://sub.example.com/token-synthetic"
	if err := f.Run(context.Background()); err != nil {
		t.Fatalf("run: %v\n%+v", err, f.Steps())
	}
	if !f.Complete() {
		t.Fatalf("not complete: %+v", f.Steps())
	}
	if h.urls[SubscriptionID] == "" || h.layerText[BaseLayerID] == "" || !h.settings.TailscaleCoexist || len(h.settings.LeakGuardGroups) == 0 {
		t.Fatalf("helper state %+v", h)
	}
	if !strings.Contains(h.layerText[BaseLayerID], "listen: 127.0.0.1:53") {
		t.Fatal("base layer is not the built-in template")
	}
	if !h.dns.Pointed || !h.tsInst || len(*opened) != 1 {
		t.Fatalf("dns %v ts %v opened %v", h.dns.Pointed, h.tsInst, *opened)
	}
	if ws := f.Step(StepMihomo).Warnings; len(ws) != 1 || !strings.Contains(ws[0], "geo") {
		t.Fatalf("geo warning %v", ws)
	}
	// The subscription is fetched before the kernel download.
	sub, kern := slices.Index(h.calls, rpc.MethodSubRefresh), slices.Index(h.calls, rpc.MethodKernelInstall)
	if sub < 0 || kern < sub {
		t.Fatalf("order %v", h.calls)
	}

	// A second flow on the same machine finds everything done but health.
	g, _, _ := newTestFlow(h, &installed, Options{})
	g.Env.Tailnet = &fakeTailnet{backend: "Running"}
	g.Detect(context.Background())
	for _, s := range g.Steps() {
		if s.ID != StepHealth && s.State != Done {
			t.Errorf("re-run: %s is %s", s.ID, s.State)
		}
	}
}

func TestFlowDownloadRetryThroughProxy(t *testing.T) {
	h := newFakeHelper()
	h.kernelErr = &rpc.Error{Code: rpc.CodeFailed, Message: "GitHub: connection reset"}
	installed := true
	f, _, _ := newTestFlow(h, &installed, Options{SubscriptionURL: "https://sub.example.com/x"})
	err := f.Run(context.Background())
	var se *StepError
	if !errors.As(err, &se) || se.Step != StepMihomo || se.Code != CodeDownload {
		t.Fatalf("err = %v", err)
	}
	if s := f.Step(StepMihomo); s.State != Failed || !strings.Contains(s.Error, "connection reset") {
		t.Fatalf("step %+v", s)
	}
	// The user picks the subscription's proxy and retries that step.
	h.kernelErr = nil
	f.Opts.Proxy = rpc.ProxySubscription
	if err := f.RunStep(context.Background(), StepMihomo); err != nil {
		t.Fatal(err)
	}
	if h.proxies[len(h.proxies)-1] != rpc.ProxySubscription {
		t.Fatalf("proxies %v", h.proxies)
	}
	if err := f.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFlowNotRootAndHealthFail(t *testing.T) {
	h := newFakeHelper()
	h.dryRun = true
	installed := true
	f, _, _ := newTestFlow(h, &installed, Options{SubscriptionURL: "https://sub.example.com/x"})
	err := f.Run(context.Background())
	var se *StepError
	if !errors.As(err, &se) || se.Step != StepApply || se.Code != CodeNotRoot {
		t.Fatalf("err = %v", err)
	}
	h.dryRun = false
	h.health = append(h.health, health.CheckReport{ID: "system-dns", Title: "System DNS", Result: health.Result{Status: health.StatusFail}})
	err = f.Run(context.Background())
	if !errors.As(err, &se) || se.Step != StepHealth || se.Code != CodeHealthFail || !strings.Contains(se.Error(), "System DNS") {
		t.Fatalf("err = %v", err)
	}
}

func TestFlowSkipsUnsupportedDNSAndTailscale(t *testing.T) {
	h := newFakeHelper()
	h.dns.Supported = false
	installed := true
	f, _, _ := newTestFlow(h, &installed, Options{SubscriptionURL: "https://sub.example.com/x", NoTailscale: true})
	f.Detect(context.Background())
	if err := f.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []Step{StepDNS, StepTailscale, StepLogin} {
		if f.Step(id).State != Skipped {
			t.Errorf("%s: %+v", id, f.Step(id))
		}
	}
	if h.tsInst || h.dns.Pointed {
		t.Fatal("touched a skipped step")
	}
}

func exportZip(t *testing.T, manifest *Manifest, files map[string]string) *Import {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for n, c := range files {
		w, _ := zw.Create(n)
		w.Write([]byte(c))
	}
	if manifest != nil {
		w, _ := zw.Create(ManifestName)
		json.NewEncoder(w).Encode(manifest)
	}
	zw.Close()
	im, err := ReadExport(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return im
}

func TestFlowImport(t *testing.T) {
	im := exportZip(t, &Manifest{Version: 1,
		Layers: []ManifestLayer{
			{ID: "my-base", Name: "My base", Stage: "base", Format: "merge", Enabled: true},
			{ID: "cloudflared-direct", Name: "cloudflared direct", Stage: "user", Format: "merge", Enabled: true},
			{ID: "old", Name: "Old", Stage: "user", Format: "merge", Enabled: false},
		},
		Subscriptions: []rpc.SubExport{{ID: "cloud", Name: "Example Cloud", Active: true, IntervalMinutes: 720, URL: "https://sub.example.com/synthetic"}},
		Settings:      &ManifestSetting{LeakGuardGroups: []string{"AI"}, TailscaleCoexist: true},
	}, map[string]string{
		"config.yaml":                       "proxies: []\n",
		"overrides/my-base.yaml":            "mixed-port: 7899\n",
		"overrides/cloudflared-direct.yaml": "+rules:\n  - PROCESS-NAME,cloudflared,DIRECT\n",
		"overrides/old.yaml":                "{}\n",
		"../evil.yaml":                      "x",
	})
	if !im.Manifest || !im.HasURL() || !im.HasBase() || len(im.Layers) != 3 {
		t.Fatalf("import %+v", im)
	}
	h := newFakeHelper()
	installed := true
	f, _, _ := newTestFlow(h, &installed, Options{Import: im, NoTailscale: true})
	if err := f.Run(context.Background()); err != nil {
		t.Fatalf("%v %+v", err, f.Steps())
	}
	if h.urls["cloud"] != "https://sub.example.com/synthetic" || h.layerText[BaseLayerID] != "" {
		t.Fatalf("urls %v, built-in base added: %q", h.urls, h.layerText[BaseLayerID])
	}
	ids := []string{}
	for _, l := range h.layers {
		ids = append(ids, l.ID)
	}
	if !slices.Equal(ids, []string{"my-base", "cloudflared-direct", "old"}) || h.layers[2].Enabled {
		t.Fatalf("layers %+v", h.layers)
	}
	if !slices.Equal(h.settings.LeakGuardGroups, []string{"AI"}) {
		t.Fatalf("leak guard %v", h.settings.LeakGuardGroups)
	}
}

func TestImportWithoutManifestOrURL(t *testing.T) {
	im := exportZip(t, nil, map[string]string{
		"config.yaml":         "proxies:\n  - {name: a, type: http, server: 192.0.2.1, port: 80}\n",
		"overrides/mine.yaml": "+rules:\n  - DOMAIN,example.com,DIRECT\n",
	})
	if im.Manifest || im.HasURL() || len(im.Layers) != 1 || im.Layers[0].Stage != "user" {
		t.Fatalf("%+v", im)
	}
	h := newFakeHelper()
	installed := true
	f, _, _ := newTestFlow(h, &installed, Options{Import: im, NoTailscale: true, NoDNS: true})
	if err := f.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.content[SubscriptionID] == "" || len(f.Step(StepSubscription).Warnings) != 1 {
		t.Fatalf("offline import: %v %+v", h.content, f.Step(StepSubscription))
	}
	if _, err := ReadExport(bytes.NewReader([]byte("not a zip")), 9); err == nil {
		t.Fatal("read garbage")
	}
}

func TestWriteExportRoundTrip(t *testing.T) {
	h := newFakeHelper()
	h.layers = []rpc.Layer{{ID: "base", Name: "Base", Stage: "base", Format: "merge", Enabled: true}, {ID: "tailscale-coexist", Managed: true}}
	h.layerText["base"] = "mixed-port: 7899\n"
	ex := &exportHelper{fakeHelper: h}
	var b bytes.Buffer
	if err := WriteExport(context.Background(), ex, &b, ExportOptions{SubscriptionURLs: true}); err != nil {
		t.Fatal(err)
	}
	im, err := ReadExport(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(im.Layers) != 1 || im.Layers[0].Content != "mixed-port: 7899\n" || !im.HasURL() || string(im.Config) != "live: true\n" {
		t.Fatalf("%+v", im)
	}
	// Without the URLs: names only.
	b.Reset()
	WriteExport(context.Background(), ex, &b, ExportOptions{})
	im, _ = ReadExport(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if im.HasURL() {
		t.Fatal("URL exported without the option")
	}
}

type exportHelper struct{ *fakeHelper }

func (e *exportHelper) Call(ctx context.Context, method string, params, result any) error {
	switch method {
	case rpc.MethodConfigGet:
		*(result.(*rpc.ConfigText)) = rpc.ConfigText{Content: "live: true\n"}
		return nil
	case rpc.MethodLayerGet:
		id := params.(rpc.IDParams).ID
		*(result.(*rpc.LayerContent)) = rpc.LayerContent{Content: e.layerText[id]}
		return nil
	case rpc.MethodSubExport:
		*(result.(*[]rpc.SubExport)) = []rpc.SubExport{{ID: "main", URL: "https://sub.example.com/x"}}
		return nil
	case rpc.MethodSubList:
		*(result.(*[]rpc.Subscription)) = []rpc.Subscription{{ID: "main"}}
		return nil
	}
	return e.fakeHelper.Call(ctx, method, params, result)
}
