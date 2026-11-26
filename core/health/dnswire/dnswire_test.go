package dnswire

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"testing"
	"time"
)

// respond builds a response to query q with the given rcode and A records.
func respond(q []byte, rcode byte, addrs ...netip.Addr) []byte {
	r := append([]byte(nil), q...)
	r[2] |= 0x80
	r[3] = 0x80 | rcode
	binary.BigEndian.PutUint16(r[6:], uint16(len(addrs)))
	for _, a := range addrs {
		r = append(r, 0xC0, 12) // pointer to question name
		if a.Is4() {
			r = binary.BigEndian.AppendUint16(r, TypeA)
		} else {
			r = binary.BigEndian.AppendUint16(r, TypeAAAA)
		}
		r = binary.BigEndian.AppendUint16(r, 1)
		r = binary.BigEndian.AppendUint32(r, 60)
		b := a.AsSlice()
		r = binary.BigEndian.AppendUint16(r, uint16(len(b)))
		r = append(r, b...)
	}
	return r
}

func TestBuildParse(t *testing.T) {
	q, err := BuildQuery(0x1234, "peer.example.ts.net.", TypeA)
	if err != nil {
		t.Fatal(err)
	}
	want := netip.MustParseAddr("100.64.1.2")
	a, err := ParseResponse(respond(q, 0, want), 0x1234)
	if err != nil {
		t.Fatal(err)
	}
	if a.Rcode != "NOERROR" || len(a.Addrs) != 1 || a.Addrs[0] != want {
		t.Fatalf("got %+v", a)
	}
	a, err = ParseResponse(respond(q, 2), 0x1234)
	if err != nil || a.Rcode != "SERVFAIL" || len(a.Addrs) != 0 {
		t.Fatalf("servfail: %+v %v", a, err)
	}
	if _, err := ParseResponse(respond(q, 0), 1); err == nil {
		t.Fatal("expected id mismatch")
	}
	if _, err := BuildQuery(1, "a..b", TypeA); err == nil {
		t.Fatal("expected bad name")
	}
}

func TestQueryUDP(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer pc.Close()
	fake := netip.MustParseAddr("198.18.0.7")
	go func() {
		buf := make([]byte, 512)
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		pc.WriteTo(respond(buf[:n], 0, fake), addr)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a, err := Query(ctx, pc.LocalAddr().String(), "short", TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Addrs) != 1 || a.Addrs[0] != fake {
		t.Fatalf("got %+v", a)
	}
}
