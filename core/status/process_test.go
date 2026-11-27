package status

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
)

// A synthetic `ps -axo pid=,rss=,pcpu=,etime=,comm=`.
const psFixture = `    1  12000   0.0 10-01:24:59 /sbin/launchd
  210  19728   0.4 10-01:24:59 /opt/homebrew/opt/tailscale/bin/tailscaled
 4242  37504   3.7 09-20:38:36 /usr/local/bin/mihomo
 5000   7152   0.0       01:42 /Applications/Some App.app/Contents/MacOS/Some App
 bad line
 6000   x   0.0       01:42 /bin/zsh
`

func TestParsePS(t *testing.T) {
	list := ParsePS(psFixture)
	if len(list) != 4 {
		t.Fatalf("got %d entries: %+v", len(list), list)
	}
	m := list[2]
	want := 9*24*time.Hour + 20*time.Hour + 38*time.Minute + 36*time.Second
	if m.PID != 4242 || m.RSS != 37504*1024 || m.CPU != 3.7 || m.Elapsed != want || m.Command != "/usr/local/bin/mihomo" {
		t.Errorf("mihomo %+v", m)
	}
	if c := list[3].Command; c != "/Applications/Some App.app/Contents/MacOS/Some App" {
		t.Errorf("command with spaces %q", c)
	}
	if list[3].Elapsed != 102*time.Second {
		t.Errorf("mm:ss %v", list[3].Elapsed)
	}
}

func TestParseEtime(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"00:05":      5 * time.Second,
		"01:00:00":   time.Hour,
		"2-00:00:01": 48*time.Hour + time.Second,
	} {
		if got, err := parseEtime(in); err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "5", "a:b", "x-01:00"} {
		if _, err := parseEtime(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestProcessMatch(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/usr/local/bin/mihomo":              true,
		"/opt/mihomo/mihomo-darwin-arm64-v2": true,
		"/usr/bin/clash-meta":                true,
		"/tmp/h/bin/fakemihomo":              false,
		"/Applications/duet.app/duet":        false,
	} {
		if IsMihomoCommand(cmd) != want {
			t.Errorf("mihomo %s: %v", cmd, !want)
		}
	}
	for cmd, want := range map[string]bool{
		"/opt/homebrew/opt/tailscale/bin/tailscaled":                            true,
		"/Library/SystemExtensions/x/io.tailscale.ipn.macsys.network-extension": true,
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale":                  false,
	} {
		if IsTailscaleDaemon(cmd) != want {
			t.Errorf("tailscale %s: %v", cmd, !want)
		}
	}
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	p := FindProcess(ParsePS(psFixture), now, IsMihomoCommand)
	if p == nil || p.PID != 4242 || p.Uptime(now) != 9*24*time.Hour+20*time.Hour+38*time.Minute+36*time.Second {
		t.Errorf("find %+v", p)
	}
	if FindProcess(nil, now, IsMihomoCommand) != nil {
		t.Error("found in nothing")
	}
}

func TestDuration(t *testing.T) {
	i18n.Set("zh-Hans")
	defer i18n.Set("zh-Hans")
	for d, want := range map[time.Duration]string{
		0:                             "1 分钟",
		5 * time.Minute:               "5 分钟",
		3*time.Hour + 12*time.Minute:  "3 小时 12 分",
		9*24*time.Hour + 20*time.Hour: "9 天 20 小时",
	} {
		if got := Duration(d); got != want {
			t.Errorf("%v: %q", d, got)
		}
	}
	i18n.Set("en")
	if got := Duration(26 * time.Hour); got != "1d 2h" {
		t.Errorf("en %q", got)
	}
}

// The monitor reads mihomo's memory from the controller's /connections
// (a fake controller), its process and tailscaled's from the process
// list, and the DNS mode from the config file.
func TestMonitorProcessesAndMemory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "v1.0.0", "meta": true})
		case "/configs":
			_ = json.NewEncoder(w).Encode(map[string]any{"mode": "rule", "mixed-port": 7890})
		case "/proxies":
			_ = json.NewEncoder(w).Encode(map[string]any{"proxies": proxies()})
		case "/connections":
			_ = json.NewEncoder(w).Encode(map[string]any{"connections": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}, "memory": 48 << 20})
		case "/traffic":
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	mc, err := mihomo.New(mihomo.Options{Address: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	dns := "fake-ip"
	m := NewMonitor(Options{
		Mihomo: mc, MihomoPoll: 20 * time.Millisecond, ProcessPoll: 20 * time.Millisecond, DNSPoll: 20 * time.Millisecond,
		Now:       func() time.Time { return now },
		Processes: func(context.Context) ([]ProcessEntry, error) { return ParsePS(psFixture), nil },
		MihomoFile: func(context.Context) (*health.MihomoConfig, error) {
			return &health.MihomoConfig{DNS: health.MihomoDNSConfig{Enable: dns != "", EnhancedMode: dns}}, nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Run(ctx)
	s := waitFor(t, m, "memory", func(s Snapshot) bool {
		return s.Mihomo.Memory != 0 && s.Mihomo.Process != nil && s.Tailscale.Process != nil && s.Mihomo.DNSMode != ""
	})
	if s.Mihomo.Memory != 48<<20 || s.Mihomo.Connections != 2 || s.Mihomo.Config.MixedPort != 7890 {
		t.Errorf("mihomo %+v", s.Mihomo)
	}
	if p := s.Mihomo.Process; p.PID != 4242 || p.RSS != 37504*1024 {
		t.Errorf("mihomo process %+v", p)
	}
	if p := s.Tailscale.Process; p.PID != 210 || p.CPU != 0.4 {
		t.Errorf("tailscaled %+v", p)
	}
	if s.Mihomo.DNSMode != "fake-ip" {
		t.Errorf("dns %q", s.Mihomo.DNSMode)
	}
}

func TestPollDNSModes(t *testing.T) {
	for _, tc := range []struct {
		cfg  *health.MihomoConfig
		err  error
		want string
	}{
		{&health.MihomoConfig{}, nil, DNSModeOff},
		{&health.MihomoConfig{DNS: health.MihomoDNSConfig{Enable: true}}, nil, DNSModeNormal},
		{&health.MihomoConfig{DNS: health.MihomoDNSConfig{Enable: true, EnhancedMode: "Redir-Host"}}, nil, "redir-host"},
		{nil, errors.New("no file"), ""},
	} {
		m := NewMonitor(Options{Mihomo: &fakeMihomo{}, MihomoFile: func(context.Context) (*health.MihomoConfig, error) { return tc.cfg, tc.err }})
		m.pollDNS(context.Background())
		if got := m.Snapshot().Mihomo.DNSMode; got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.cfg, got, tc.want)
		}
	}
}

// A failed process read keeps the last answer.
func TestPollProcessesKeepsLast(t *testing.T) {
	fail := false
	m := NewMonitor(Options{Mihomo: &fakeMihomo{}, Processes: func(context.Context) ([]ProcessEntry, error) {
		if fail {
			return nil, errors.New("ps failed")
		}
		return ParsePS(psFixture), nil
	}})
	m.pollProcesses(context.Background())
	fail = true
	m.pollProcesses(context.Background())
	if m.Snapshot().Mihomo.Process == nil {
		t.Error("lost the process on a failed read")
	}
}
