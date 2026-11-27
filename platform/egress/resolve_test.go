package egress

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"runtime"
	"strings"
	"testing"
)

func addrs(ss ...string) []netip.Addr {
	out := make([]netip.Addr, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParseAddr(s)
	}
	return out
}

// The network's own servers come first, without mihomo's listener, fake-ip
// addresses, MagicDNS, Windows' placeholders or duplicates; the public
// fallbacks follow.
func TestDNSCandidates(t *testing.T) {
	got := DNSCandidates(addrs("127.0.0.1", "192.168.1.1", "198.18.0.2", "100.100.100.100",
		"fec0:0:0:ffff::1", "::1", "0.0.0.0", "fe80::1", "192.168.1.1", "223.5.5.5", "2001:db8::53"))
	want := append(addrs("192.168.1.1", "223.5.5.5", "2001:db8::53"), addrs("119.29.29.29", "1.1.1.1", "8.8.8.8")...)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%v\nwant %v", got, want)
	}
	if got := DNSCandidates(nil); fmt.Sprint(got) != fmt.Sprint(PublicDNS) {
		t.Fatalf("no own servers: %v", got)
	}
}

func TestResolveTriesServersInOrder(t *testing.T) {
	var asked []string
	lookup := func(_ context.Context, s netip.Addr, host string) ([]netip.Addr, error) {
		asked = append(asked, s.String())
		if host != "echo.example" {
			t.Errorf("host %q", host)
		}
		switch s.String() {
		case "192.168.1.1":
			return nil, errors.New("refused")
		case "192.168.1.2":
			return nil, nil // no A record from this one
		}
		return addrs("203.0.113.80"), nil
	}
	got, err := Resolve(context.Background(), "echo.example", Path{DNS: addrs("127.0.0.1", "192.168.1.1", "192.168.1.2")}, lookup)
	if err != nil || fmt.Sprint(got) != "[203.0.113.80]" {
		t.Fatal(got, err)
	}
	if strings.Join(asked, " ") != "192.168.1.1 192.168.1.2 223.5.5.5" {
		t.Errorf("asked %v", asked)
	}
	// An address needs no lookup.
	if got, err := Resolve(context.Background(), "1.1.1.1", Path{}, func(context.Context, netip.Addr, string) ([]netip.Addr, error) {
		t.Fatal("looked up an address")
		return nil, nil
	}); err != nil || got[0].String() != "1.1.1.1" {
		t.Fatal(got, err)
	}
	// Every server failing names each one.
	_, err = Resolve(context.Background(), "echo.example", Path{}, func(context.Context, netip.Addr, string) ([]netip.Addr, error) {
		return nil, errors.New("timeout")
	})
	if err == nil || !strings.Contains(err.Error(), "223.5.5.5: timeout") || !strings.Contains(err.Error(), "8.8.8.8: timeout") {
		t.Fatal(err)
	}
}

// The direct request resolves a hostname with the path's servers, never
// with the system resolver (mihomo's fake-ip DNS), and still sends the
// hostname as the TLS name.
func TestDirectResolvesOverThePath(t *testing.T) {
	var sni string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sni = r.TLS.ServerName
		fmt.Fprint(w, "Current IP Address: 203.0.113.9\n")
	}))
	srv.StartTLS()
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	tlsConf := srv.Client().Transport.(*http.Transport).TLSClientConfig // trusts the cert for example.com
	lookup := func(_ context.Context, s netip.Addr, host string) ([]netip.Addr, error) {
		if host != "example.com" {
			t.Errorf("host %q", host)
		}
		return addrs("127.0.0.1"), nil
	}
	u := (&url.URL{Scheme: "https", Host: net.JoinHostPort("example.com", port), Path: "/"}).String()
	ip, err := direct(context.Background(), u, Path{DNS: addrs("192.168.1.1")}, &net.Dialer{}, lookup, tlsConf)
	if err != nil || ip.String() != "203.0.113.9" {
		t.Fatal(ip, err)
	}
	if sni != "example.com" {
		t.Errorf("TLS name %q", sni)
	}
	if _, err := direct(context.Background(), "http://example.com/", Path{}, &net.Dialer{}, lookup, tlsConf); err == nil {
		t.Error("http accepted")
	}
}

// PinnedLookup really asks the given server over a socket pinned to the
// interface: a tiny DNS server on 127.0.0.1 answers when the socket is
// pinned to loopback.
func TestPinnedLookup(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("runs where the tests run: darwin and linux")
	}
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { pc.Close() })
	go serveA(pc, netip.MustParseAddr("203.0.113.80"))
	_, p, _ := net.SplitHostPort(pc.LocalAddr().String())
	old := dnsPort
	dnsPort = p
	t.Cleanup(func() { dnsPort = old })
	got, err := PinnedLookup(loopback(t))(context.Background(), netip.MustParseAddr("127.0.0.1"), "echo.example")
	if err != nil || fmt.Sprint(got) != "[203.0.113.80]" {
		t.Fatal(got, err)
	}
}

// serveA answers every DNS query on pc with one A record (a for A
// questions, no answers otherwise).
func serveA(pc net.PacketConn, a netip.Addr) {
	buf := make([]byte, 1500)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		q := buf[:n]
		if n < 12 {
			continue
		}
		// The question: name labels, then type and class.
		i := 12
		for i < n && q[i] != 0 {
			i += int(q[i]) + 1
		}
		if i+5 > n {
			continue
		}
		qtype := binary.BigEndian.Uint16(q[i+1:])
		question := q[12 : i+5]
		resp := append([]byte{}, q[0:2]...)
		resp = append(resp, 0x81, 0x80, 0, 1) // response, RD RA; 1 question
		if qtype == 1 {
			resp = append(resp, 0, 1, 0, 0, 0, 0)
		} else {
			resp = append(resp, 0, 0, 0, 0, 0, 0)
		}
		resp = append(resp, question...)
		if qtype == 1 {
			ip := a.As4()
			resp = append(resp, 0xc0, 12, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4)
			resp = append(resp, ip[:]...)
		}
		pc.WriteTo(resp, from)
	}
}
