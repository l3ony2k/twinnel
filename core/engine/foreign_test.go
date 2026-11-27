package engine

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
)

// Synthetic Clash Party process holding the controller port.
func clashPartyProc() *foreign.Process {
	return &foreign.Process{PID: 5150, PPID: 5100,
		Exe:       "/Applications/Clash Party.app/Contents/Resources/sidecar/mihomo",
		ParentExe: "/Applications/Clash Party.app/Contents/MacOS/Clash Party",
		Args:      []string{"mihomo", "-d", "/Users/someone/Library/Application Support/mihomo-party/work"}}
}

// portOwners is a switchable PortOwner.
type portOwners struct {
	mu sync.Mutex
	by map[int]*foreign.Process
}

func (p *portOwners) set(port int, proc *foreign.Process) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.by == nil {
		p.by = map[int]*foreign.Process{}
	}
	p.by[port] = proc
}

func (p *portOwners) lookup(_ context.Context, port int) (*foreign.Process, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.by[port], nil
}

func (h *harness) ctlPort() int { return foreign.Port(h.ctrlSrv.Listener.Addr().String()) }

func (h *harness) managedProc() *foreign.Process {
	st, _ := h.e.Store.Settings()
	return &foreign.Process{PID: 4242, Exe: st.Mihomo.Binary}
}

func TestApplyRefusesForeignController(t *testing.T) {
	h := newHarness(t)
	owners := &portOwners{}
	h.e.PortOwner = owners.lookup
	owners.set(h.ctlPort(), h.managedProc())
	h.apply("first")
	if o := h.e.Owner(); o.Verdict != foreign.Managed {
		t.Fatalf("owner after first apply: %+v", o)
	}
	before := h.disk()
	reloads, _, _ := h.ctrl.State()

	owners.set(h.ctlPort(), clashPartyProc())
	h.e.PutLayer(rpc.LayerPutParams{ID: "my-rules", Content: "+rules:\n  - DOMAIN-SUFFIX,example.net,DIRECT\n"})
	r, err := h.e.Apply(context.Background(), rpc.ApplyParams{Reason: "second"})
	if !errors.Is(err, ErrForeignController) {
		t.Fatalf("err = %v", err)
	}
	if r.Written || r.Reloaded || !r.Foreign.IsForeign() || r.Foreign.Client != foreign.ClientClashParty || r.Foreign.PID != 5150 {
		t.Fatalf("result: %+v / %+v", r, r.Foreign)
	}
	if r.ErrorMsg == nil || r.ErrorMsg.Code != "controller-foreign-app" || r.ErrorMsg.Args[1] != "Clash Party" {
		t.Errorf("error msg: %+v", r.ErrorMsg)
	}
	if after, _, _ := h.ctrl.State(); len(after) != len(reloads) {
		t.Errorf("reloaded a foreign controller: %d → %d", len(reloads), len(after))
	}
	if !bytes.Equal(h.disk(), before) {
		t.Error("the config was written for a foreign controller")
	}
	st, _ := h.e.Status(context.Background())
	if !st.Mihomo.Owner.IsForeign() || st.Mihomo.Owner.Client != foreign.ClientClashParty {
		t.Errorf("status owner: %+v", st.Mihomo.Owner)
	}
}

// safePathsServer is a foreign mihomo: /version answers, PUT /configs is
// refused like Clash Party's mihomo refused Twinnel's path (synthetic).
func safePathsServer(t *testing.T, puts *int) *httptest.Server {
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			w.Write([]byte(`{"meta":true,"version":"v1.19.34"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/configs":
			mu.Lock()
			*puts++
			mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"message":"path is not subpath of home directory or SAFE_PATHS: /var/lib/twinnel/mihomo/config.yaml\n allowed paths: [/home/someone/.config/mihomo-party/work]"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSafePathsRefusalHoldsAutoApply(t *testing.T) {
	h := newHarness(t)
	h.apply("first") // Twinnel's mihomo, before the other client started
	before := h.disk()

	puts := 0
	foreignSrv := safePathsServer(t, &puts)
	h.e.NewController = func(addr, secret string) Controller {
		return &HTTPController{Addr: foreignSrv.Listener.Addr().String(), Secret: secret}
	}
	h.e.PutLayer(rpc.LayerPutParams{ID: "my-rules", Content: "+rules:\n  - DOMAIN-SUFFIX,example.net,DIRECT\n"})
	r, err := h.e.Apply(context.Background(), rpc.ApplyParams{Reason: "after the other client started"})
	if !errors.Is(err, ErrForeignController) {
		t.Fatalf("err = %v", err)
	}
	if puts != 1 {
		t.Errorf("PUTs to the foreign controller = %d, want 1 (no reload of the restored file)", puts)
	}
	if r.Written || !r.RolledBack || !r.Foreign.IsForeign() || r.Foreign.Via != foreign.ViaSafePaths || r.Foreign.Client != foreign.ClientClashParty {
		t.Fatalf("result %+v / %+v", r, r.Foreign)
	}
	// The raw refusal is kept in the details, never in the message.
	if strings.Contains(r.Error, "SAFE_PATHS") || strings.Contains(r.ErrorMsg.Text, "SAFE_PATHS") || !strings.Contains(r.Foreign.Detail, "SAFE_PATHS") {
		t.Errorf("error %q / detail %q", r.Error, r.Foreign.Detail)
	}
	if !bytes.Equal(h.disk(), before) {
		t.Error("the previous config was not put back")
	}

	// A tailnet change does not auto-apply while the controller is foreign.
	status, _ := os.ReadFile("testdata/tailscale-status.json")
	h.tailnet.set(bytes.ReplaceAll(bytes.ReplaceAll(status, []byte(`"nas"`), []byte(`"nas2"`)), []byte("nas.example"), []byte("nas2.example")))
	out, err := h.e.RefreshTailnet(context.Background())
	if err != nil || !out.Changed {
		t.Fatalf("refresh: %+v %v", out, err)
	}
	if out.Apply != nil || puts != 1 {
		t.Fatalf("auto-apply ran against the foreign controller: %+v, puts %d", out.Apply, puts)
	}
	// Without a port lookup, the SAFE_PATHS verdict stays.
	if o := h.e.ControllerOwner(context.Background()); !o.IsForeign() {
		t.Fatalf("verdict lost: %+v", o)
	}

	// The other client quits; Twinnel's mihomo answers again: the held
	// auto-apply runs by itself.
	h.e.NewController = func(addr, secret string) Controller {
		return &HTTPController{Addr: h.ctrlSrv.Listener.Addr().String(), Secret: secret}
	}
	reloads, _, _ := h.ctrl.State()
	owners := &portOwners{}
	owners.set(h.ctlPort(), h.managedProc())
	h.e.PortOwner = owners.lookup
	if o := h.e.ControllerOwner(context.Background()); o.Verdict != foreign.Managed {
		t.Fatalf("owner: %+v", o)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if now, _, _ := h.ctrl.State(); len(now) > len(reloads) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the held auto-apply did not run")
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.e.resumed.Wait()
	if !bytes.Contains(h.disk(), []byte("nas2")) {
		t.Error("the resumed apply did not carry the tailnet change")
	}
}

// installedManager is fakeManager with the service installed.
type installedManager struct{ fakeManager }

func (m *installedManager) Status(_ context.Context, l string) (service.Status, error) {
	return service.Status{Label: l, Installed: true, Loaded: true}, nil
}

func TestMoveController(t *testing.T) {
	h := newHarness(t)
	h.apply("first")
	owners := &portOwners{}
	old := h.ctlPort()
	owners.set(old, clashPartyProc())
	h.e.PortOwner = owners.lookup
	h.e.Services = &installedManager{}
	ctx := context.Background()

	// A taken port is refused; so is the port it moves away from.
	busy, _ := net.Listen("tcp", "127.0.0.1:0")
	defer busy.Close()
	if _, err := h.e.MoveController(ctx, rpc.ControllerMoveParams{Port: busy.Addr().(*net.TCPAddr).Port}); err == nil {
		t.Error("moved onto a busy port")
	}
	if _, err := h.e.MoveController(ctx, rpc.ControllerMoveParams{Port: old}); err == nil {
		t.Error("moved onto the same port")
	}

	dry, err := h.e.MoveController(ctx, rpc.ControllerMoveParams{DryRun: true})
	if err != nil || !dry.DryRun || dry.From != "127.0.0.1:"+strconv.Itoa(old) || foreign.Port(dry.To) < firstMovePort {
		t.Fatalf("dry run: %+v %v", dry, err)
	}
	if st, _ := h.e.Store.Settings(); st.Mihomo.Controller != h.ctrlSrv.Listener.Addr().String() {
		t.Fatal("a dry run changed settings")
	}

	r, err := h.e.MoveController(ctx, rpc.ControllerMoveParams{})
	if err != nil {
		t.Fatalf("move: %v (%+v)", err, r)
	}
	if r.To != dry.To || r.Apply == nil || !r.Apply.Written || !r.Restarted {
		t.Errorf("move: %+v / apply %+v", r, r.Apply)
	}
	st, _ := h.e.Store.Settings()
	if st.Mihomo.Controller != r.To {
		t.Errorf("settings controller = %s", st.Mihomo.Controller)
	}
	if !bytes.Contains(h.disk(), []byte("external-controller: "+r.To)) {
		t.Error("the config does not carry the new port")
	}
	if b, err := h.e.Store.ReadLayer(ControllerPortLayerID); err != nil || !strings.Contains(string(b), r.To) {
		t.Errorf("layer: %q %v", b, err)
	}
	if hist, _ := h.e.History(); hist[0].ReasonCode != store.ReasonCtlMove {
		t.Errorf("history reason %q", hist[0].ReasonCode)
	}
	// Nothing Twinnel does now goes to the old port.
	if o := h.e.ControllerOwner(ctx); o.Addr != r.To || o.IsForeign() {
		t.Errorf("owner after move: %+v", o)
	}
}
