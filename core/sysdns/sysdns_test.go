package sysdns

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"slices"
	"testing"
)

type fakePlatform struct {
	svcs []Service
	sets []string
	fail string
}

func (f *fakePlatform) Services(context.Context) ([]Service, error) {
	return slices.Clone(f.svcs), nil
}

func (f *fakePlatform) Set(_ context.Context, name string, servers []string) error {
	if name == f.fail {
		return errors.New("refused")
	}
	for i := range f.svcs {
		if f.svcs[i].Name == name {
			f.svcs[i].Servers = slices.Clone(servers)
		}
	}
	f.sets = append(f.sets, name)
	return nil
}

var mihomo = []string{"127.0.0.1"}

func TestPointRestore(t *testing.T) {
	p := &fakePlatform{svcs: []Service{{Name: "Wi-Fi"}, {Name: "USB LAN", Servers: []string{"192.0.2.53"}}}}
	m := &Manager{P: p, Path: filepath.Join(t.TempDir(), "dns-backup.json"), Target: mihomo}
	st, err := m.Point(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Pointed || len(st.Changed) != 2 || st.Backup == nil || len(st.Backup.Services) != 2 {
		t.Fatalf("status %+v", st)
	}
	// Pointing again changes nothing and keeps the original backup.
	st, err = m.Point(context.Background())
	if err != nil || len(st.Changed) != 0 {
		t.Fatalf("second point: %+v %v", st, err)
	}
	if bk, _ := m.ReadBackup(); !slices.Equal(bk.Services[1].Servers, []string{"192.0.2.53"}) {
		t.Fatalf("backup overwritten: %+v", bk)
	}
	// A service that appears later is added to the backup.
	p.svcs = append(p.svcs, Service{Name: "Ethernet", Servers: []string{"198.51.100.1"}})
	if _, err := m.Point(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, err = m.Restore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Backup != nil || len(st.Changed) != 3 {
		t.Fatalf("restore %+v", st)
	}
	want := []Service{{Name: "Wi-Fi", Servers: nil}, {Name: "USB LAN", Servers: []string{"192.0.2.53"}}, {Name: "Ethernet", Servers: []string{"198.51.100.1"}}}
	for i, s := range p.svcs {
		if s.Name != want[i].Name || !slices.Equal(s.Servers, want[i].Servers) {
			t.Fatalf("after restore %+v", p.svcs)
		}
	}
	if _, err := m.Restore(context.Background()); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("second restore: %v", err)
	}
}

func TestPointFailureKeepsBackup(t *testing.T) {
	p := &fakePlatform{svcs: []Service{{Name: "Wi-Fi", Servers: []string{"192.0.2.1"}}, {Name: "Ethernet"}}, fail: "Ethernet"}
	m := &Manager{P: p, Path: filepath.Join(t.TempDir(), "dns-backup.json"), Target: mihomo}
	if _, err := m.Point(context.Background()); err == nil {
		t.Fatal("no error")
	}
	p.fail = ""
	if _, err := m.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.svcs[0].Servers, []string{"192.0.2.1"}) {
		t.Fatalf("not restored: %+v", p.svcs)
	}
}

func TestPointNeedsServices(t *testing.T) {
	m := &Manager{P: &fakePlatform{}, Path: filepath.Join(t.TempDir(), "b.json"), Target: mihomo}
	if _, err := m.Point(context.Background()); err == nil {
		t.Fatal("pointed with no services")
	}
	m.Target = nil
	if _, err := m.Point(context.Background()); err == nil {
		t.Fatal("pointed at nothing")
	}
	if _, err := m.Adopt(context.Background()); err == nil {
		t.Fatal("adopted with no target")
	}
}

var winTarget = []string{"127.0.0.1", "::1"}

func TestPointsAt(t *testing.T) {
	for _, c := range []struct {
		servers, mihomo []string
		want            bool
	}{
		{nil, mihomo, false},
		{[]string{"127.0.0.1"}, mihomo, true},
		{[]string{"127.0.0.1"}, winTarget, true}, // set by hand without ::1
		{[]string{"127.0.0.1", "::1"}, winTarget, true},
		{[]string{"::1"}, winTarget, true},
		{[]string{"127.0.0.1", "192.0.2.1"}, mihomo, false},
		{[]string{"192.0.2.1"}, mihomo, false},
		{[]string{"127.0.0.1"}, nil, false},
	} {
		if got := PointsAt(c.servers, c.mihomo); got != c.want {
			t.Errorf("PointsAt(%v, %v) = %v", c.servers, c.mihomo, got)
		}
	}
}

func TestMihomoAddrs(t *testing.T) {
	for _, c := range []struct {
		host, goos string
		want       []string
	}{
		{"127.0.0.1", "darwin", []string{"127.0.0.1"}},
		{"127.0.0.1", "windows", []string{"127.0.0.1", "::1"}},
		{"0.0.0.0", "darwin", []string{"127.0.0.1"}},
		{"", "windows", []string{"127.0.0.1", "::1"}},
		{"127.0.0.53", "linux", []string{"127.0.0.53"}},
	} {
		if got := MihomoAddrs(c.host, c.goos); !slices.Equal(got, c.want) {
			t.Errorf("MihomoAddrs(%q, %q) = %v, want %v", c.host, c.goos, got, c.want)
		}
	}
	// What dns.point sets counts as pointed under the health check's
	// definition on every platform.
	if !PointsAt(winTarget, MihomoAddrs("127.0.0.1", "windows")) || !PointsAt(mihomo, MihomoAddrs("127.0.0.1", "darwin")) {
		t.Fatal("dns.point's target is not mihomo by MihomoAddrs")
	}
}

// D0: the DNS already pointed at mihomo (by hand) before Twinnel. Point
// must not record the target as what to restore: it records automatic,
// and Restore then gives the service working DNS back.
func TestPointNeverBacksUpTheTarget(t *testing.T) {
	p := &fakePlatform{svcs: []Service{{Name: "Wi-Fi", Servers: []string{"127.0.0.1"}}, {Name: "Ethernet", Servers: []string{"192.0.2.53"}}}}
	m := &Manager{P: p, Path: filepath.Join(t.TempDir(), "dns-backup.json"), Target: winTarget}
	ctx := context.Background()
	st, err := m.Point(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(st.Recorded, []string{"Wi-Fi"}) || len(st.Unprotected) != 0 || !st.Pointed {
		t.Fatalf("point: %+v", st)
	}
	bk, _ := m.ReadBackup()
	if bk.Services[0].Servers != nil || !slices.Equal(bk.Services[1].Servers, []string{"192.0.2.53"}) {
		t.Fatalf("backup %+v", bk)
	}
	if _, err := m.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if p.svcs[0].Servers != nil || !slices.Equal(p.svcs[1].Servers, []string{"192.0.2.53"}) {
		t.Fatalf("after restore %+v", p.svcs)
	}
}

// A backup an older Twinnel wrote with the target in it is repaired by
// the next Point, and Restore never writes the target back meanwhile.
func TestBadBackupRepaired(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"restore", "point"} {
		p := &fakePlatform{svcs: []Service{{Name: "Wi-Fi", Servers: []string{"127.0.0.1"}}}}
		m := &Manager{P: p, Path: filepath.Join(t.TempDir(), "dns-backup.json"), Target: mihomo}
		if err := m.writeBackup(&Backup{Services: []Service{{Name: "Wi-Fi", Servers: []string{"127.0.0.1"}}}}); err != nil {
			t.Fatal(err)
		}
		if st, _ := m.Status(ctx); len(st.Unprotected) != 0 {
			t.Fatalf("an entry (even a bad one) protects: %+v", st)
		}
		if op == "point" {
			if _, err := m.Point(ctx); err != nil {
				t.Fatal(err)
			}
			if bk, _ := m.ReadBackup(); bk.Services[0].Servers != nil {
				t.Fatalf("not repaired: %+v", bk)
			}
		}
		if _, err := m.Restore(ctx); err != nil {
			t.Fatal(err)
		}
		if p.svcs[0].Servers != nil {
			t.Fatalf("%s: restored to %v", op, p.svcs[0].Servers)
		}
	}
}

// Adopt: pointed services without a backup entry get automatic recorded;
// no DNS setting changes; services that do not point at mihomo are not
// recorded (they are the user's, nothing to restore).
func TestAdopt(t *testing.T) {
	p := &fakePlatform{svcs: []Service{{Name: "Wi-Fi", Servers: []string{"127.0.0.1"}}, {Name: "Ethernet", Servers: []string{"192.0.2.53"}}}}
	m := &Manager{P: p, Path: filepath.Join(t.TempDir(), "dns-backup.json"), Target: winTarget}
	ctx := context.Background()
	st, err := m.Status(ctx)
	if err != nil || st.Pointed || !slices.Equal(st.Unprotected, []string{"Wi-Fi"}) || st.Backup != nil {
		t.Fatalf("before: %+v %v", st, err)
	}
	st, err = m.Adopt(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.sets) != 0 {
		t.Fatalf("adopt changed DNS: %v", p.sets)
	}
	if !slices.Equal(st.Recorded, []string{"Wi-Fi"}) || len(st.Unprotected) != 0 || st.Backup == nil || len(st.Backup.Services) != 1 {
		t.Fatalf("after: %+v", st)
	}
	// Again: nothing to record.
	if st, err = m.Adopt(ctx); err != nil || len(st.Recorded) != 0 {
		t.Fatalf("second adopt: %+v %v", st, err)
	}
	// Restore puts Wi-Fi on automatic and leaves Ethernet alone.
	if _, err := m.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if p.svcs[0].Servers != nil || !slices.Equal(p.svcs[1].Servers, []string{"192.0.2.53"}) || !slices.Equal(p.sets, []string{"Wi-Fi"}) {
		t.Fatalf("after restore %+v sets %v", p.svcs, p.sets)
	}
}

func TestLinkConnected(t *testing.T) {
	a := func(ss ...string) []netip.Addr {
		var out []netip.Addr
		for _, s := range ss {
			out = append(out, netip.MustParseAddr(s))
		}
		return out
	}
	cases := []struct {
		name        string
		up, carrier bool
		addrs       []netip.Addr
		want        bool
	}{
		{"connected", true, true, a("192.0.2.10", "fe80::1"), true},
		{"down", false, false, a("192.0.2.10"), false},
		{"no carrier", true, false, a("192.0.2.10"), false},
		{"no lease (link-local only)", true, true, a("169.254.10.20", "fe80::1"), false},
		{"no address", true, true, nil, false},
		{"IPv6 only", true, true, a("2001:db8::10"), false},
	}
	for _, c := range cases {
		if got := LinkConnected(c.up, c.carrier, c.addrs); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// A disconnected service on automatic DNS (a Bluetooth PAN) does not hold
// the status at "not pointed", and is not unprotected; dns.point still
// sets it, so it resolves through mihomo once it connects.
func TestStatusCountsConnectedOnly(t *testing.T) {
	p := &fakePlatform{svcs: []Service{
		{Name: "Wi-Fi", Servers: []string{"127.0.0.1"}, Connected: true},
		{Name: "Bluetooth PAN", Connected: false},
		{Name: "Ethernet", Servers: []string{"127.0.0.1"}, Connected: false},
	}}
	m := &Manager{P: p, Path: filepath.Join(t.TempDir(), "dns-backup.json"), Target: mihomo}
	st, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Pointed || !slices.Equal(st.Unprotected, []string{"Wi-Fi"}) {
		t.Fatalf("status: %+v", st)
	}
	// Adopt records exactly the unprotected ones (what the fix's plan
	// lists); dns.point records the rest.
	st, err = m.Adopt(context.Background())
	if err != nil || !slices.Equal(st.Recorded, []string{"Wi-Fi"}) || len(st.Unprotected) != 0 || !st.Pointed {
		t.Fatalf("adopt: %+v %v", st, err)
	}
	if bk, _ := m.ReadBackup(); slices.ContainsFunc(bk.Services, func(s Service) bool { return s.Connected }) {
		t.Fatalf("backup stores link state: %+v", bk)
	}
	st, err = m.Point(context.Background())
	if err != nil || !slices.Equal(st.Changed, []string{"Bluetooth PAN"}) || !slices.Equal(st.Recorded, []string{"Ethernet"}) || !st.Pointed {
		t.Fatalf("point: %+v %v", st, err)
	}
	if bk, _ := m.ReadBackup(); len(bk.Services) != 3 {
		t.Fatalf("backup after point: %+v", bk)
	}

	// The connected one off mihomo: not pointed, whatever the others say.
	p.svcs[0].Servers = []string{"192.0.2.53"}
	if st, _ := m.Status(context.Background()); st.Pointed {
		t.Fatalf("pointed with Wi-Fi on 192.0.2.53: %+v", st)
	}
}

// Offline (nothing connected) every service counts.
func TestCountedOffline(t *testing.T) {
	svcs := []Service{{Name: "Wi-Fi", Servers: []string{"127.0.0.1"}}, {Name: "Ethernet"}}
	if got := Counted(svcs); len(got) != 2 {
		t.Fatalf("offline: %+v", got)
	}
	svcs[1].Connected = true
	if got := Counted(svcs); len(got) != 1 || got[0].Name != "Ethernet" {
		t.Fatalf("one connected: %+v", got)
	}
}
