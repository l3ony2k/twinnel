package windows

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/sysdns"
)

// fakeNetsh keeps hand-set servers per GUID and carries out the netsh
// commands SystemDNS runs, so Services sees what Set did.
type fakeNetsh struct {
	byIndex map[uint32]string // adapter index -> GUID
	v4, v6  map[string][]string
	calls   []string
}

func (f *fakeNetsh) StaticDNS(guid string) ([]string, []string, error) {
	return f.v4[guid], f.v6[guid], nil
}

func (f *fakeNetsh) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, filepath.Base(strings.ReplaceAll(name, `\`, "/"))+" "+strings.Join(args, " "))
	kv := map[string]string{}
	for _, a := range args[4:] {
		k, v, _ := strings.Cut(a, "=")
		kv[k] = v
	}
	var idx uint32
	for _, c := range kv["name"] {
		idx = idx*10 + uint32(c-'0')
	}
	guid := f.byIndex[idx]
	m := f.v4
	if args[1] == "ipv6" {
		m = f.v6
	}
	switch {
	case args[2] == "set" && kv["source"] == "dhcp":
		delete(m, guid)
	case args[2] == "set":
		m[guid] = []string{kv["address"]}
	case args[2] == "add":
		m[guid] = append(m[guid], kv["address"])
	}
	return nil, nil
}

// dnsSys: Wi-Fi (DHCP), Ethernet (unplugged, hand-set 9.9.9.9 and
// 2001:db8::53), Tailscale, mihomo's TUN, WSL's vEthernet, a Hyper-V
// external switch carrying the default route, a VMware host-only adapter
// and the loopback.
func dnsSys() (*fakeSys, *fakeNetsh) {
	s := coexistSys()
	s.adapters = append(s.adapters,
		Adapter{Index: 41, GUID: "{00000000-0000-4000-8000-000000000041}", Name: "vEthernet (WSL)", Description: "Hyper-V Virtual Ethernet Adapter", IfType: IfTypeEthernet, Up: true, Addrs: ips("172.20.0.1")},
		Adapter{Index: 42, GUID: "{00000000-0000-4000-8000-000000000042}", Name: "vEthernet (External)", Description: "Hyper-V Virtual Ethernet Adapter #2", IfType: IfTypeEthernet, Up: true, Addrs: ips("10.0.0.20"), Gateways: ips("10.0.0.1")},
		Adapter{Index: 43, GUID: "{00000000-0000-4000-8000-000000000043}", Name: "VMware Network Adapter VMnet1", Description: "VMware Virtual Ethernet Adapter for VMnet1", IfType: IfTypeEthernet, Up: true, Addrs: ips("192.168.100.1")},
	)
	n := &fakeNetsh{
		byIndex: map[uint32]string{},
		v4:      map[string][]string{"{00000000-0000-4000-8000-000000000012}": {"9.9.9.9"}},
		v6:      map[string][]string{"{00000000-0000-4000-8000-000000000012}": {"2001:db8::53"}},
	}
	for _, a := range s.adapters {
		n.byIndex[a.Index] = a.GUID
	}
	return s, n
}

func TestDNSManaged(t *testing.T) {
	s, _ := dnsSys()
	var got []string
	for _, a := range s.adapters {
		if DNSManaged(a) {
			got = append(got, a.Name)
		}
	}
	want := []string{"Wi-Fi", "Ethernet", "vEthernet (External)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("managed = %v, want %v", got, want)
	}
}

func TestSystemDNSPointRestore(t *testing.T) {
	s, n := dnsSys()
	p := SystemDNS{Sys: s, Static: n, Run: n.run}
	m := &sysdns.Manager{P: p, Path: filepath.Join(t.TempDir(), "dns-backup.json"), Target: p.DNSTarget()}
	ctx := context.Background()

	st, err := m.Point(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Pointed || len(st.Changed) != 3 {
		t.Fatalf("after point: %+v", st)
	}
	for _, g := range []string{"{00000000-0000-4000-8000-000000000011}", "{00000000-0000-4000-8000-000000000012}", "{00000000-0000-4000-8000-000000000042}"} {
		if !reflect.DeepEqual(n.v4[g], []string{"127.0.0.1"}) || !reflect.DeepEqual(n.v6[g], []string{"::1"}) {
			t.Fatalf("%s: v4 %v v6 %v", g, n.v4[g], n.v6[g])
		}
	}
	for _, g := range []string{"{00000000-0000-4000-8000-000000000021}", "{00000000-0000-4000-8000-000000000031}", "{00000000-0000-4000-8000-000000000041}", "{00000000-0000-4000-8000-000000000043}"} {
		if n.v4[g] != nil || n.v6[g] != nil {
			t.Fatalf("%s (tunnel or host-only) was changed", g)
		}
	}
	for _, c := range n.calls {
		if !strings.HasPrefix(c, "netsh.exe interface ipv") || strings.Contains(c, "set") && strings.Contains(c, "static") && !strings.Contains(c, "validate=no") {
			t.Fatalf("unexpected call %q", c)
		}
	}

	// Pointing again changes nothing.
	n.calls = nil
	if st, err := m.Point(ctx); err != nil || len(st.Changed) != 0 || len(n.calls) != 0 {
		t.Fatalf("second point: %+v %v calls %v", st, err, n.calls)
	}

	st, err = m.Restore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Changed) != 3 {
		t.Fatalf("restore changed %v", st.Changed)
	}
	if n.v4["{00000000-0000-4000-8000-000000000011}"] != nil || n.v6["{00000000-0000-4000-8000-000000000011}"] != nil {
		t.Fatal("Wi-Fi did not go back to DHCP")
	}
	if !reflect.DeepEqual(n.v4["{00000000-0000-4000-8000-000000000012}"], []string{"9.9.9.9"}) ||
		!reflect.DeepEqual(n.v6["{00000000-0000-4000-8000-000000000012}"], []string{"2001:db8::53"}) {
		t.Fatalf("Ethernet not restored: %v %v", n.v4, n.v6)
	}
}

func TestSystemDNSSetCommands(t *testing.T) {
	s, n := dnsSys()
	p := SystemDNS{Sys: s, Static: n, Run: n.run}
	if err := p.Set(context.Background(), "Wi-Fi", []string{"1.1.1.1", "8.8.8.8", "2001:db8::1"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"netsh.exe interface ipv4 set dnsservers name=11 source=static address=1.1.1.1 validate=no",
		"netsh.exe interface ipv4 add dnsservers name=11 address=8.8.8.8 index=2 validate=no",
		"netsh.exe interface ipv6 set dnsservers name=11 source=static address=2001:db8::1 validate=no",
	}
	if !reflect.DeepEqual(n.calls, want) {
		t.Fatalf("calls:\n%s", strings.Join(n.calls, "\n"))
	}
	for _, bad := range [][]string{{"dns.example"}, {"fe80::1%11"}} {
		if err := p.Set(context.Background(), "Wi-Fi", bad); err == nil {
			t.Fatalf("Set(%v) accepted", bad)
		}
	}
	for _, name := range []string{"Tailscale", "Meta", "vEthernet (WSL)", "Nope"} {
		if err := p.Set(context.Background(), name, []string{"127.0.0.1"}); err == nil {
			t.Fatalf("Set(%s) accepted", name)
		}
	}
}

func TestSystemDNSNoRunner(t *testing.T) {
	s, n := dnsSys()
	p := SystemDNS{Sys: s, Static: n}
	if err := p.Set(context.Background(), "Wi-Fi", []string{"127.0.0.1"}); err == nil {
		t.Fatal("Set without a runner succeeded")
	}
}

func TestParseNameServer(t *testing.T) {
	got := ParseNameServer("192.0.2.1,192.0.2.2 2001:db8::1;bogus")
	want := []string{"192.0.2.1", "192.0.2.2", "2001:db8::1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if ParseNameServer("") != nil {
		t.Fatal("empty value gave servers")
	}
}

func TestWithoutDefaultV6DNS(t *testing.T) {
	got := addrStrings(withoutDefaultV6DNS(ips("127.0.0.1", "fec0:0:0:ffff::1", "fec0:0:0:ffff::3", "::1", "fec0::4")))
	want := []string{"127.0.0.1", "::1", "fec0::4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}
