package setup

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/clilink"
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
	tsRes     *rpc.TailscaleInstallResult // nil: the open-source tailscaled installed by Twinnel
	health    []health.CheckReport
	login     *fakeTailnet // tailscaled behind the helper
	inSession bool         // Windows: tailscale.login refuses Start
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
		if h.tsRes != nil {
			return out(*h.tsRes)
		}
		return out(rpc.TailscaleInstallResult{Version: "1.104.7", Kind: "twinnel", Path: "/S/twinnel/tailscale/current/tailscale"})
	case rpc.MethodTailscaleLogin:
		if h.login == nil {
			return errors.New("no tailscaled")
		}
		ls, err := h.login.LoginState(context.Background())
		if err != nil {
			return err
		}
		if params.(rpc.TailscaleLoginParams).Start && ls.Backend != "Running" && h.inSession {
			return &rpc.Error{Code: rpc.CodeNotImplemented, Message: "Tailscale logs in from the signed-in user's session"}
		}
		if params.(rpc.TailscaleLoginParams).Start && ls.Backend != "Running" {
			h.login.StartLogin(context.Background())
			ls, _ = h.login.LoginState(context.Background())
		}
		return out(rpc.TailscaleLoginResult{BackendState: ls.Backend, AuthURL: ls.AuthURL, Tailnet: ls.Tailnet})
	case rpc.MethodTailnetRefresh:
		return nil
	case rpc.MethodHealthRun:
		b, _ := json.Marshal(health.Report{Checks: h.health})
		return out(rpc.HealthReport{Report: b})
	}
	return errors.New("unexpected method " + method)
}

// loginState is what the fake tailscaled reports.
type loginState struct{ Backend, AuthURL, Tailnet string }

type fakeTailnet struct {
	backend string
	polls   int
	started bool
}

func (t *fakeTailnet) LoginState(context.Context) (loginState, error) {
	t.polls++
	if t.started && t.polls > 3 {
		t.backend = "Running"
	}
	ls := loginState{Backend: t.backend, Tailnet: "example.ts.net"}
	if t.started && t.backend != "Running" {
		ls.AuthURL = "https://login.tailscale.com/a/synthetic"
	}
	return ls, nil
}

func (t *fakeTailnet) StartLogin(context.Context) error { t.started = true; return nil }

func newTestFlow(h *fakeHelper, installed *bool, o Options) (*Flow, *fakeTailnet, *[]string) {
	tn := &fakeTailnet{backend: "NeedsLogin"}
	h.login = tn
	var opened []string
	env := Env{
		Connect: func(context.Context) (Caller, error) {
			if !*installed {
				return nil, errors.New("no socket")
			}
			return h, nil
		},
		InstallHelper: func(context.Context) error { *installed = true; return nil },
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
	if ws := f.Step(StepMihomo).Warnings; len(ws) != 1 || ws[0].Code != WarnGeo {
		t.Fatalf("geo warning %v", ws)
	}
	// The subscription is fetched before the kernel download.
	sub, kern := slices.Index(h.calls, rpc.MethodSubRefresh), slices.Index(h.calls, rpc.MethodKernelInstall)
	if sub < 0 || kern < sub {
		t.Fatalf("order %v", h.calls)
	}

	// A second flow on the same machine finds everything done but health.
	g, _, _ := newTestFlow(h, &installed, Options{})
	h.login = &fakeTailnet{backend: "Running"}
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

func TestFlowImportHook(t *testing.T) {
	h := newFakeHelper()
	installed := true
	f, _, _ := newTestFlow(h, &installed, Options{ImportZip: []byte("PK synthetic"), NoTailscale: true, NoDNS: true})
	// No config.import in this build: a clear error, nothing imported.
	err := f.Run(context.Background())
	if !errors.Is(err, ErrNoImport) || f.Step(StepSubscription).State != Failed {
		t.Fatalf("err = %v", err)
	}
	var got []byte
	f.Env.ImportConfig = func(_ context.Context, hc Caller, zip []byte) (ImportResult, error) {
		got = zip
		hc.Call(context.Background(), rpc.MethodSubPut, rpc.SubPutParams{ID: "cloud", Name: "Example Cloud", Content: "proxies: []\n"}, nil)
		return ImportResult{Subscription: true, LeakGuardGroups: []string{"AI"}}, nil
	}
	if err := f.Run(context.Background()); err != nil {
		t.Fatalf("%v %+v", err, f.Steps())
	}
	if string(got) != "PK synthetic" || h.layerText[BaseLayerID] == "" || !slices.Equal(h.settings.LeakGuardGroups, []string{"AI"}) {
		t.Fatalf("zip %q, helper %+v", got, h.settings)
	}
	// A backup without a subscription asks for the URL.
	g, _, _ := newTestFlow(newFakeHelper(), &installed, Options{ImportZip: []byte("x")})
	g.Env.ImportConfig = func(context.Context, Caller, []byte) (ImportResult, error) { return ImportResult{}, nil }
	var se *StepError
	if err := g.Run(context.Background()); !errors.As(err, &se) || se.Code != CodeNeedsSubscription {
		t.Fatalf("err = %v", err)
	}
}

func TestFlowLinksTailscale(t *testing.T) {
	h := newFakeHelper()
	installed := true
	f, _, _ := newTestFlow(h, &installed, Options{SubscriptionURL: "https://sub.example.com/x"})
	var linked []string
	f.Env.LinkTailscale = func(cli string) (string, error) {
		linked = append(linked, cli)
		return "", &clilink.OnPathError{Path: "/opt/homebrew/bin/tailscale"}
	}
	if err := f.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	ws := f.Step(StepTailscale).Warnings
	if len(linked) != 1 || linked[0] != "/S/twinnel/tailscale/current/tailscale" || len(ws) != 1 || ws[0].Code != WarnTSOnPath {
		t.Fatalf("linked %v warnings %+v", linked, ws)
	}
}

// Windows: the official client from its MSI. A failed install is not a
// download failure (another proxy would not help); a pending restart is a
// warning; nothing is linked onto PATH (the MSI does that itself).
func TestFlowWindowsTailscale(t *testing.T) {
	h := newFakeHelper()
	h.tsErr = &rpc.Error{Code: rpc.CodeFailed, Message: "another installation is in progress (Windows Installer is busy); wait for it to finish and try again"}
	installed := true
	f, _, _ := newTestFlow(h, &installed, Options{SubscriptionURL: "https://sub.example.com/x"})
	linked := 0
	f.Env.LinkTailscale = func(string) (string, error) { linked++; return "", nil }
	err := f.Run(context.Background())
	var se *StepError
	if !errors.As(err, &se) || se.Step != StepTailscale || se.Code != CodeInstallFailed {
		t.Fatalf("err = %v", err)
	}
	// A download failure still offers a proxy.
	h.tsErr = &rpc.Error{Code: rpc.CodeFailed, Message: `Get "https://pkgs.tailscale.com/stable/?mode=json": dial tcp: i/o timeout`}
	if err := f.RunStep(context.Background(), StepTailscale); !errors.As(err, &se) || se.Code != CodeDownload {
		t.Fatalf("err = %v", err)
	}
	h.tsErr = nil
	h.tsRes = &rpc.TailscaleInstallResult{Kind: "windows", Version: "1.104.7", Path: `C:\Program Files\Tailscale\tailscale.exe`, RestartNeeded: true}
	if err := f.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	ws := f.Step(StepTailscale).Warnings
	if len(ws) != 1 || ws[0].Code != WarnRestart || linked != 0 || f.Step(StepTailscale).Detail != "1.104.7" {
		t.Fatalf("warnings %+v linked %d", ws, linked)
	}
}

// mihomo that does not run on this CPU is an install failure.
func TestFlowKernelDoesNotRun(t *testing.T) {
	h := newFakeHelper()
	h.kernelErr = &rpc.Error{Code: rpc.CodeFailed, Message: "the downloaded mihomo does not run on this computer: exit status 1"}
	installed := true
	f, _, _ := newTestFlow(h, &installed, Options{SubscriptionURL: "https://sub.example.com/x"})
	var se *StepError
	if err := f.Run(context.Background()); !errors.As(err, &se) || se.Step != StepMihomo || se.Code != CodeInstallFailed {
		t.Fatalf("err = %v", err)
	}
}

// Windows: the helper refuses to start the login; the flow starts it from
// its own process (the signed-in user) and follows it through the helper.
func TestFlowSessionLogin(t *testing.T) {
	h := newFakeHelper()
	h.inSession = true
	installed := true
	f, tn, opened := newTestFlow(h, &installed, Options{SubscriptionURL: "https://sub.example.com/x"})
	// Without the hook the step fails.
	err := f.Run(context.Background())
	var se *StepError
	if !errors.As(err, &se) || se.Step != StepLogin {
		t.Fatalf("no hook: %v", err)
	}
	sessions := 0
	tn.polls = 0
	f.Env.SessionLogin = func(ctx context.Context) error {
		sessions++
		return tn.StartLogin(ctx)
	}
	if err := f.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || len(*opened) != 1 || f.Step(StepLogin).Detail != "example.ts.net" {
		t.Fatalf("sessions %d opened %v step %+v", sessions, *opened, f.Step(StepLogin))
	}
}
