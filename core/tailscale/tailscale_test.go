package tailscale

import (
	"context"
	"errors"
	"io"
	"net/netip"
	"testing"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// fakeAPI is an in-memory LocalAPI. All data is synthetic.
type fakeAPI struct {
	status *ipnstate.Status
	prefs  *ipn.Prefs
	edits  []*ipn.MaskedPrefs
	notes  []ipn.Notify
}

func (f *fakeAPI) Status(context.Context) (*ipnstate.Status, error) { return f.status, nil }
func (f *fakeAPI) GetPrefs(context.Context) (*ipn.Prefs, error)     { return f.prefs, nil }
func (f *fakeAPI) EditPrefs(_ context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	f.edits = append(f.edits, mp)
	if mp.WantRunningSet {
		f.prefs.WantRunning = mp.WantRunning
	}
	if mp.ExitNodeIDSet {
		f.prefs.ExitNodeID = mp.ExitNodeID
	}
	return f.prefs, nil
}
func (f *fakeAPI) WatchIPNBus(context.Context, ipn.NotifyWatchOpt) (Watcher, error) {
	return &fakeWatcher{notes: f.notes}, nil
}

type fakeWatcher struct{ notes []ipn.Notify }

func (w *fakeWatcher) Next() (ipn.Notify, error) {
	if len(w.notes) == 0 {
		return ipn.Notify{}, io.EOF
	}
	n := w.notes[0]
	w.notes = w.notes[1:]
	return n, nil
}
func (w *fakeWatcher) Close() error { return nil }

func nodeKey() key.NodePublic { return key.NewNode().Public() }

func fixture() *fakeAPI {
	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	self := &ipnstate.PeerStatus{
		ID: "nSelf", HostName: "laptop", DNSName: "laptop.example-tailnet.ts.net.", OS: "macOS",
		TailscaleIPs: []netip.Addr{netip.MustParseAddr("fd7a:115c:a1e0::1"), netip.MustParseAddr("100.64.0.1")},
		Online:       true, UserID: 7, KeyExpiry: &exp,
	}
	peers := map[key.NodePublic]*ipnstate.PeerStatus{
		nodeKey(): {ID: "nB", HostName: "box", DNSName: "box.example-tailnet.ts.net.", OS: "linux",
			TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.2")}, Online: true, Active: true,
			CurAddr: "192.0.2.10:41641", Relay: "tok", ExitNodeOption: true},
		nodeKey(): {ID: "nA", HostName: "alpha", DNSName: "alpha.example-tailnet.ts.net.", OS: "windows",
			TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.3")}, Online: true, Active: true, Relay: "sin", ExitNode: true},
		nodeKey(): {ID: "nZ", HostName: "zeta", OS: "iOS", Online: false,
			LastSeen: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		nodeKey(): {ID: "nS", HostName: "shared", ShareeNode: true, Online: true},
	}
	return &fakeAPI{
		status: &ipnstate.Status{
			Version: "1.90.0", BackendState: "Running", TUN: true, Self: self, Peer: peers,
			MagicDNSSuffix: "example-tailnet.ts.net",
			CurrentTailnet: &ipnstate.TailnetStatus{Name: "user@example.com"},
			User:           map[tailcfg.UserID]tailcfg.UserProfile{7: {LoginName: "user@example.com"}},
			ExitNodeStatus: &ipnstate.ExitNodeStatus{ID: "nA", Online: true},
		},
		prefs: &ipn.Prefs{WantRunning: true, CorpDNS: true, ExitNodeID: "nA"},
	}
}

func TestState(t *testing.T) {
	f := fixture()
	c := New(f, false)
	now := time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	s, err := c.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !s.Running() || s.Version != "1.90.0" || s.User != "user@example.com" || !s.Fetched.Equal(now) {
		t.Errorf("state %+v", s)
	}
	if s.Self.Name != "laptop" || s.Self.IPv4() != netip.MustParseAddr("100.64.0.1") {
		t.Errorf("self %+v", s.Self)
	}
	if len(s.Peers) != 3 {
		t.Fatalf("want 3 peers (sharee hidden), got %d", len(s.Peers))
	}
	order := s.Peers[0].Name + "," + s.Peers[1].Name + "," + s.Peers[2].Name
	if order != "alpha,box,zeta" {
		t.Errorf("order %s", order)
	}
	if s.OnlinePeers() != 2 {
		t.Errorf("online %d", s.OnlinePeers())
	}
	if p := s.Peers[1].Path(); p != "direct" {
		t.Errorf("box path %s", p)
	}
	if p := s.Peers[0].Path(); p != "derp" {
		t.Errorf("alpha path %s", p)
	}
	if p := s.Peers[2].Path(); p != "idle" || s.Peers[2].Name != "zeta" || s.Peers[2].LastSeen.IsZero() {
		t.Errorf("zeta %+v", s.Peers[2])
	}
	if s.ExitNode == nil || s.ExitNode.Name != "alpha" || !s.ExitNodeOnline {
		t.Errorf("exit node %+v", s.ExitNode)
	}
	if !s.Prefs.MagicDNS || s.Prefs.ExitNodeID != "nA" {
		t.Errorf("prefs %+v", s.Prefs)
	}
	if !s.KeyExpiresWithin(now, 14*24*time.Hour) || s.KeyExpiresWithin(now, time.Hour) {
		t.Error("key expiry")
	}
}

func TestWritesDisabled(t *testing.T) {
	f := fixture()
	c := New(f, false)
	ctx := context.Background()
	for _, err := range []error{c.Up(ctx), c.Down(ctx), c.SetExitNode(ctx, ""), c.SetMagicDNS(ctx, false), c.SetShieldsUp(ctx, true)} {
		if !errors.Is(err, ErrWritesDisabled) {
			t.Errorf("want ErrWritesDisabled, got %v", err)
		}
	}
	if len(f.edits) != 0 {
		t.Error("edit reached the API")
	}
}

func TestWrites(t *testing.T) {
	f := fixture()
	c := New(f, true)
	ctx := context.Background()
	if err := c.Down(ctx); err != nil || f.prefs.WantRunning || !f.edits[0].WantRunningSet {
		t.Fatalf("down: %v %+v", err, f.edits)
	}
	if err := c.Up(ctx); err != nil || !f.prefs.WantRunning {
		t.Fatalf("up: %v", err)
	}
	if err := c.SetExitNode(ctx, ""); err != nil || f.prefs.ExitNodeID != "" || !f.edits[2].ExitNodeIPSet {
		t.Fatalf("exit node: %v", err)
	}
	if err := c.SetMagicDNS(ctx, false); err != nil || !f.edits[3].CorpDNSSet || f.edits[3].CorpDNS {
		t.Fatalf("magicdns: %v", err)
	}
	if err := c.SetShieldsUp(ctx, true); err != nil || !f.edits[4].ShieldsUpSet || !f.edits[4].ShieldsUp {
		t.Fatalf("shields: %v", err)
	}
}

func TestWatch(t *testing.T) {
	f := fixture()
	running := ipn.Running
	f.notes = []ipn.Notify{
		{State: &running},
		{Version: "x"}, // irrelevant
		{PeersRemoved: []tailcfg.NodeID{3}},
	}
	n := 0
	err := New(f, false).Watch(context.Background(), func() { n++ })
	if !errors.Is(err, io.EOF) || n != 2 {
		t.Errorf("err %v, calls %d", err, n)
	}
}

func TestDetectInstall(t *testing.T) {
	set := func(paths ...string) Probe {
		m := map[string]bool{}
		for _, p := range paths {
			m[p] = true
		}
		return func(p string) bool { return m[p] }
	}
	cases := []struct {
		name  string
		goos  string
		probe Probe
		want  Install
	}{
		{"brew", "darwin", set("/var/run/tailscaled.socket", "/opt/homebrew/bin/tailscaled"),
			Install{Kind: InstallTailscaled, Source: "brew", Daemon: "/opt/homebrew/bin/tailscaled"}},
		{"app store", "darwin", set("/Applications/Tailscale.app", "/Applications/Tailscale.app/Contents/_MASReceipt/receipt"),
			Install{Kind: InstallMacAppStore, Source: "/Applications/Tailscale.app", UsesSystemVPN: true}},
		{"standalone", "darwin", set("/Applications/Tailscale.app"),
			Install{Kind: InstallMacStandalone, Source: "/Applications/Tailscale.app", UsesSystemVPN: true}},
		{"daemon wins over app", "darwin", set("/Applications/Tailscale.app", "/var/run/tailscaled.socket"),
			Install{Kind: InstallTailscaled}},
		{"installed not running", "darwin", set("/usr/local/bin/tailscaled"),
			Install{Kind: InstallTailscaled, Source: "brew-or-binary", Daemon: "/usr/local/bin/tailscaled"}},
		{"nothing", "darwin", set(), Install{Kind: InstallUnknown}},
		{"linux pkg", "linux", set("/run/tailscale/tailscaled.sock", "/usr/sbin/tailscaled"),
			Install{Kind: InstallTailscaled, Source: "package", Daemon: "/usr/sbin/tailscaled"}},
		{"windows", "windows", set(), Install{Kind: InstallWindows}},
		{"plan9", "plan9", set(), Install{Kind: InstallUnknown}},
	}
	for _, tc := range cases {
		if got := DetectInstall(tc.goos, tc.probe); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
