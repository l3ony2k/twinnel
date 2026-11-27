package status

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/tailscale"
)

// --- fakes (synthetic data) ---

type fakeMihomo struct {
	mu      sync.Mutex
	err     error
	proxies map[string]mihomo.Proxy
	tun     bool
	traffic []mihomo.Traffic
	logs    []mihomo.LogEntry
	tested  []string
}

func (f *fakeMihomo) Version(context.Context) (mihomo.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return mihomo.Version{Version: "v1.0.0"}, f.err
}
func (f *fakeMihomo) Configs(context.Context) (mihomo.Config, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return mihomo.Config{Mode: mihomo.ModeRule, Tun: mihomo.Tun{Enable: f.tun, Device: "utun9"}}, f.err
}
func (f *fakeMihomo) Proxies(context.Context) (map[string]mihomo.Proxy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// A copy, as a real client decodes a new map per call: GroupDelay
	// writes f.proxies while the monitor reads the last answer.
	return maps.Clone(f.proxies), f.err
}
func (f *fakeMihomo) Connections(context.Context) (mihomo.Connections, error) {
	return mihomo.Connections{Connections: make([]mihomo.Connection, 3)}, nil
}
func (f *fakeMihomo) StreamTraffic(ctx context.Context, fn func(mihomo.Traffic)) error {
	for _, t := range f.traffic {
		fn(t)
	}
	<-ctx.Done()
	return ctx.Err()
}
func (f *fakeMihomo) StreamLogs(ctx context.Context, level string, fn func(mihomo.LogEntry)) error {
	for _, e := range f.logs {
		if level == "warning" && e.Type == "info" {
			continue
		}
		fn(e)
	}
	<-ctx.Done()
	return ctx.Err()
}
func (f *fakeMihomo) Delay(_ context.Context, name, _ string, _ time.Duration) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tested = append(f.tested, name)
	return 50, nil
}
func (f *fakeMihomo) GroupDelay(_ context.Context, group, _ string, _ time.Duration) (map[string]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tested = append(f.tested, "group:"+group)
	p := f.proxies["node-b"]
	p.History = append(p.History, mihomo.DelayRecord{Delay: 77})
	f.proxies["node-b"] = p
	return map[string]int{"node-b": 77}, nil
}

type fakeTS struct {
	state tailscale.State
	err   error
}

func (f *fakeTS) State(context.Context) (tailscale.State, error) { return f.state, f.err }
func (f *fakeTS) Watch(ctx context.Context, onChange func()) error {
	onChange()
	<-ctx.Done()
	return ctx.Err()
}

func proxies() map[string]mihomo.Proxy {
	return map[string]mihomo.Proxy{
		"GLOBAL": {Name: "GLOBAL", Type: "Selector", Now: "DIRECT", All: []string{"DIRECT", "PROXY", "Auto", "node-a", "node-b"}},
		"PROXY":  {Name: "PROXY", Type: "Selector", Now: "Auto", All: []string{"Auto", "node-a", "node-b", "DIRECT"}},
		"Auto":   {Name: "Auto", Type: "URLTest", Now: "node-b", All: []string{"node-a", "node-b"}},
		"node-a": {Name: "node-a", Type: "Vless", Alive: true, History: []mihomo.DelayRecord{{Delay: 0}}},
		"node-b": {Name: "node-b", Type: "Vless", Alive: true, History: []mihomo.DelayRecord{{Delay: 120}}},
		"DIRECT": {Name: "DIRECT", Type: "Direct", Alive: true},
	}
}

// --- model ---

func TestBuildGroups(t *testing.T) {
	gs := BuildGroups(proxies())
	if len(gs) != 3 || gs[0].Name != "PROXY" || gs[2].Name != "GLOBAL" {
		t.Fatalf("groups %+v", gs)
	}
	p := gs[0]
	if strings.Join(p.Chain, ">") != "Auto>node-b" || p.Node != "node-b" || p.Delay != 120 || !p.Tested || !p.Selectable() {
		t.Errorf("PROXY %+v", p)
	}
	if m := p.Members[0]; m.Name != "Auto" || !m.IsGroup || m.Delay != 120 {
		t.Errorf("nested group member %+v", m)
	}
	if m := p.Members[1]; m.Delay != 0 || !m.Tested {
		t.Errorf("dead member %+v", m)
	}
	if m := p.Members[3]; m.Tested {
		t.Errorf("DIRECT has no test %+v", m)
	}
	if gs[1].Selectable() {
		t.Error("URLTest is not selectable")
	}
	if g := PickPrimary(gs, "nope"); g == nil || g.Name != "PROXY" {
		t.Errorf("fallback primary %v", g)
	}
	if g := PickPrimary(gs, "Auto"); g == nil || g.Name != "Auto" {
		t.Errorf("named primary %v", g)
	}
	if PickPrimary(nil, "x") != nil {
		t.Error("nil primary")
	}
}

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	soon := now.Add(3 * 24 * time.Hour)
	healthy := func() Snapshot {
		gs := BuildGroups(proxies())
		return Snapshot{
			Time: now, Loaded: true,
			Mihomo: Mihomo{Reachable: true, Config: mihomo.Config{Tun: mihomo.Tun{Enable: true}},
				Groups: gs, Primary: PickPrimary(gs, "PROXY")},
			Tailscale: Tailscale{Reachable: true, State: tailscale.State{BackendState: "Running"},
				Install: tailscale.Install{Kind: tailscale.InstallTailscaled}},
		}
	}
	cases := []struct {
		name   string
		change func(*Snapshot)
		want   Level
		reason string
	}{
		{"healthy", func(*Snapshot) {}, LevelOK, ""},
		{"loading", func(s *Snapshot) { s.Loaded = false }, LevelUnknown, ""},
		{"mihomo down", func(s *Snapshot) { s.Mihomo = Mihomo{Err: "connection refused"} }, LevelError, "connection refused"},
		{"mihomo auth", func(s *Snapshot) { s.Mihomo = Mihomo{Unauthorized: true, Err: "x"} }, LevelError, "密钥"},
		{"tun off", func(s *Snapshot) { s.Mihomo.Config.Tun.Enable = false }, LevelWarn, "TUN"},
		{"primary dead", func(s *Snapshot) {
			gs := BuildGroups(proxies())
			s.Mihomo.Primary = &gs[0]
			s.Mihomo.Primary.Delay = 0
		}, LevelWarn, "PROXY"},
		{"ts needs login", func(s *Snapshot) { s.Tailscale.State.BackendState = "NeedsLogin" }, LevelError, "登录"},
		{"ts stopped", func(s *Snapshot) { s.Tailscale.State.BackendState = "Stopped" }, LevelWarn, "断开"},
		{"ts exit node", func(s *Snapshot) { s.Tailscale.State.ExitNode = &tailscale.Peer{Name: "exit-1"} }, LevelWarn, "exit-1"},
		{"ts key", func(s *Snapshot) { s.Tailscale.State.KeyExpiry = &soon }, LevelWarn, "过期"},
		{"ts health", func(s *Snapshot) { s.Tailscale.State.Health = []string{"DNS broken"} }, LevelWarn, "DNS broken"},
		{"ts down", func(s *Snapshot) { s.Tailscale.Reachable, s.Tailscale.Err = false, "no such socket" }, LevelError, "no such socket"},
		{"ts missing", func(s *Snapshot) {
			s.Tailscale = Tailscale{Err: "no such socket", Install: tailscale.Install{Kind: tailscale.InstallUnknown}}
		}, LevelWarn, "没有找到"},
		{"ts app", func(s *Snapshot) { s.Tailscale.Install.UsesSystemVPN = true }, LevelWarn, "VPN"},
	}
	for _, tc := range cases {
		s := healthy()
		tc.change(&s)
		Evaluate(&s)
		if s.Level != tc.want {
			t.Errorf("%s: level %v, want %v (%+v)", tc.name, s.Level, tc.want, s.Reasons)
		}
		if tc.reason != "" && (len(s.Reasons) == 0 || !strings.Contains(s.Reasons[0].Text, tc.reason)) {
			t.Errorf("%s: reasons %+v, want %q", tc.name, s.Reasons, tc.reason)
		}
		if tc.want == LevelOK && len(s.Reasons) != 0 {
			t.Errorf("%s: unexpected reasons %+v", tc.name, s.Reasons)
		}
	}
}

// --- format ---

func TestRate4(t *testing.T) {
	for in, want := range map[int64]string{
		0: "  0B", 999: "999B", 1000: "1.0K", 1536: "1.5K", 12 * 1024: " 12K",
		999 * 1024: "999K", 1024 * 1024: "1.0M", 1300 * 1024: "1.3M", 500 * 1024 * 1024: "500M",
		3 << 30: "3.0G", -5: "  0B",
	} {
		got := Rate4(in)
		if got != want || len([]rune(got)) != 4 {
			t.Errorf("Rate4(%d) = %q, want %q", in, got, want)
		}
	}
	if got := TrayTitle(TrafficSample{Traffic: mihomo.Traffic{Up: 60 * 1024, Down: 1300 * 1024}}); got != "↓1.3M ↑ 60K" {
		t.Errorf("title %q", got)
	}
}

func TestFormat(t *testing.T) {
	if Bytes(512) != "512 B" || Bytes(1536) != "1.5 KB" || Bytes(3<<30) != "3.0 GB" {
		t.Error("bytes", Bytes(512), Bytes(1536), Bytes(3<<30))
	}
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		time.Second: "刚刚", 30 * time.Second: "30 秒前", 5 * time.Minute: "5 分钟前",
		3 * time.Hour: "3 小时前", 72 * time.Hour: "3 天前",
	} {
		if got := Ago(now, now.Add(-d)); got != want {
			t.Errorf("Ago(%v) = %q", d, got)
		}
	}
	if Until(now, now.Add(5*time.Hour)) != "5 小时后" || Until(now, now.Add(80*time.Hour)) != "3 天后" {
		t.Error("until")
	}
	if DelayText(88, true) != "88 ms" || DelayText(0, true) != "超时" || DelayText(0, false) != "未测" {
		t.Error("delay text")
	}
}

func TestShortError(t *testing.T) {
	wrap := fmt.Errorf("Get \"http://x\": dial tcp: %w", syscall.ECONNREFUSED)
	if ShortError(wrap) != "connection refused" {
		t.Error(ShortError(wrap))
	}
	if ShortError(context.DeadlineExceeded) != "timeout" || ShortError(nil) != "" || ShortError(io.EOF) != "EOF" {
		t.Error("short errors")
	}
}

// --- config ---

func TestConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "app.json")
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	c, err := LoadConfig(path, getenv)
	if err != nil || c.Controller != "127.0.0.1:9090" || c.PrimaryGroup != "PROXY" || !c.AllowWrites {
		t.Fatalf("defaults %+v %v", c, err)
	}
	c.Controller, c.Secret = "127.0.0.1:9999", "file-secret"
	if err := SaveConfig(path, c); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	env[EnvSecret], env[EnvReadOnly] = "env-secret", "1"
	c, _ = LoadConfig(path, getenv)
	if c.Controller != "127.0.0.1:9999" || c.Secret != "env-secret" || !c.SecretFromEnv || c.AllowWrites {
		t.Errorf("env %+v", c)
	}
	// An env secret is never written to the file.
	c.Controller = "127.0.0.1:1"
	if err := SaveConfig(path, c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "env-secret") || !strings.Contains(string(b), "file-secret") {
		t.Errorf("saved %s", b)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path, getenv); err == nil {
		t.Error("want parse error")
	}
}

// --- monitor ---

func waitFor(t *testing.T, m *Monitor, what string, ok func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s := m.Snapshot(); ok(s) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s: %+v", what, m.Snapshot())
	return Snapshot{}
}

func TestMonitor(t *testing.T) {
	fm := &fakeMihomo{proxies: proxies(), tun: true,
		traffic: []mihomo.Traffic{{Up: 1, Down: 2}, {Up: 3, Down: 4}},
		logs:    []mihomo.LogEntry{{Type: "info", Payload: "a"}, {Type: "warning", Payload: "b"}},
	}
	ft := &fakeTS{state: tailscale.State{BackendState: "Running", Peers: []tailscale.Peer{{Name: "p", Online: true}}}}
	m := NewMonitor(Options{Mihomo: fm, Tailscale: ft, MihomoPoll: 20 * time.Millisecond, TailscalePoll: 20 * time.Millisecond, WritesAllowed: true})
	if s := m.Snapshot(); s.Level != LevelUnknown || !s.WritesAllowed {
		t.Fatalf("initial %+v", s)
	}
	var notes sync.WaitGroup
	notes.Add(1)
	var once sync.Once
	unsub := m.Subscribe(func() { once.Do(notes.Done) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Run(ctx)
	notes.Wait()
	unsub()

	s := waitFor(t, m, "loaded OK", func(s Snapshot) bool { return s.Level == LevelOK })
	if s.Mihomo.Version != "v1.0.0" || s.Mihomo.Primary == nil || s.Mihomo.Primary.Node != "node-b" || s.Mihomo.Connections != 3 {
		t.Errorf("mihomo %+v", s.Mihomo)
	}
	if s.Tailscale.State.OnlinePeers() != 1 {
		t.Errorf("tailscale %+v", s.Tailscale)
	}
	waitFor(t, m, "traffic", func(s Snapshot) bool { return s.Mihomo.Traffic.Down == 4 })
	if h := m.TrafficHistory(); len(h) != 2 || h[0].Up != 1 {
		t.Errorf("history %+v", h)
	}

	// Logs stream only on demand, at the chosen level.
	if len(m.Logs()) != 0 {
		t.Error("logs before SetLogLevel")
	}
	m.SetLogLevel("warning")
	waitFor(t, m, "logs", func(Snapshot) bool { return len(m.Logs()) == 1 })
	if m.Logs()[0].Payload != "b" || m.LogLevel() != "warning" {
		t.Errorf("logs %+v", m.Logs())
	}
	m.ClearLogs()
	m.SetLogLevel("")
	if len(m.Logs()) != 0 || m.LogLevel() != "" {
		t.Error("clear")
	}

	// Delay tests show at once: the group's delay is its node's.
	if err := m.TestGroup(ctx, "Auto"); err != nil {
		t.Fatal(err)
	}
	s = m.Snapshot()
	if s.Mihomo.Primary.Delay != 50 {
		t.Errorf("delay after test %d", s.Mihomo.Primary.Delay)
	}
	if d, err := m.TestNode(ctx, "node-a"); err != nil || d != 50 {
		t.Errorf("node test %d %v", d, err)
	}

	// mihomo goes away: red, with a short reason.
	fm.mu.Lock()
	fm.err = fmt.Errorf("dial: %w", syscall.ECONNREFUSED)
	fm.mu.Unlock()
	m.Refresh()
	s = waitFor(t, m, "red", func(s Snapshot) bool { return s.Level == LevelError })
	if s.Mihomo.Err != "connection refused" || s.Mihomo.Reachable {
		t.Errorf("down %+v", s.Mihomo)
	}
	// and comes back.
	fm.mu.Lock()
	fm.err = nil
	fm.mu.Unlock()
	waitFor(t, m, "green again", func(s Snapshot) bool { return s.Level == LevelOK })
}

func TestMonitorTailscaleError(t *testing.T) {
	fm := &fakeMihomo{proxies: proxies(), tun: true}
	ft := &fakeTS{err: errors.New("dial unix /x: connect: no such file or directory")}
	m := NewMonitor(Options{Mihomo: fm, Tailscale: ft, Install: tailscale.Install{Kind: tailscale.InstallTailscaled}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Run(ctx)
	s := waitFor(t, m, "red", func(s Snapshot) bool { return s.Level == LevelError })
	if s.Tailscale.Reachable || !strings.Contains(s.Reasons[0].Text, "Tailscale") {
		t.Errorf("%+v", s)
	}
}

func TestMonitorWithoutTailscale(t *testing.T) {
	m := NewMonitor(Options{Mihomo: &fakeMihomo{proxies: proxies(), tun: true}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Run(ctx)
	waitFor(t, m, "ok", func(s Snapshot) bool { return s.Level == LevelOK })
}
