package health

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"time"
)

// Socket probes the platform Envs share for DataplaneEnv. They are plain
// Go networking with no platform code; the Envs only add what differs
// (Windows answers Ping with IcmpSendEcho).

// NetDialTCP connects to addr with an ordinary socket and closes the
// connection once the handshake completes. A refusal is ErrRefused.
func NetDialTCP(ctx context.Context, addr string) error {
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		if isRefused(err) {
			return fmt.Errorf("%w: %v", ErrRefused, err)
		}
		return err
	}
	return c.Close()
}

// NetDialViaProxy asks the HTTP proxy at proxy to CONNECT to addr and,
// with serverName set, shakes hands with TLS through the tunnel (without
// verifying: nothing is sent, the handshake only shows that bytes go end
// to end). An HTTP proxy may answer 200 before it dials, so the TLS
// handshake is what proves the outbound works.
func NetDialViaProxy(ctx context.Context, proxy, addr, serverName string) error {
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", proxy)
	if err != nil {
		return err
	}
	defer c.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(dl)
	}
	if _, err := fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", addr, addr); err != nil {
		return err
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return fmt.Errorf("CONNECT: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CONNECT: %s", resp.Status)
	}
	if serverName == "" {
		return nil
	}
	tc := tls.Client(&readerConn{Conn: c, r: br}, &tls.Config{ServerName: serverName, InsecureSkipVerify: true}) //nolint:gosec // reachability only, no data
	if err := tc.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("TLS: %w", err)
	}
	return nil
}

type readerConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *readerConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// NetPing sends one ICMP echo to ip on a raw socket and waits for the
// reply. Raw sockets need root (macOS, Linux): without, ErrNotSupported.
func NetPing(ctx context.Context, ip string) error {
	dst, err := netip.ParseAddr(ip)
	if err != nil || !dst.Is4() {
		return fmt.Errorf("ping: %q is not an IPv4 address", ip)
	}
	c, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotSupported, err)
	}
	defer c.Close()
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(time.Second)
	}
	_ = c.SetDeadline(dl)
	id := uint16(os.Getpid())
	msg := EchoRequest(id, 1)
	if _, err := c.WriteTo(msg, &net.IPAddr{IP: dst.AsSlice()}); err != nil {
		return err
	}
	buf := make([]byte, 1500)
	for {
		n, from, err := c.ReadFrom(buf)
		if err != nil {
			if IsTimeout(err) {
				return errors.New("no echo reply")
			}
			return err
		}
		fa, _ := from.(*net.IPAddr)
		if fa == nil || !fa.IP.Equal(dst.AsSlice()) {
			continue
		}
		if IsEchoReply(buf[:n], id, 1) {
			return nil
		}
	}
}

// EchoRequest is an ICMPv4 echo request with id and seq.
func EchoRequest(id, seq uint16) []byte {
	m := []byte{8, 0, 0, 0, byte(id >> 8), byte(id), byte(seq >> 8), byte(seq), 't', 'w', 'n', 'l'}
	cs := icmpChecksum(m)
	m[2], m[3] = byte(cs>>8), byte(cs)
	return m
}

// IsEchoReply reports whether b (an ICMPv4 message, or an IPv4 packet
// carrying one) is the echo reply to id and seq.
func IsEchoReply(b []byte, id, seq uint16) bool {
	if len(b) >= 20 && b[0]>>4 == 4 { // an IPv4 header in front (macOS raw sockets)
		if hl := int(b[0]&0x0f) * 4; hl >= 20 && len(b) >= hl {
			b = b[hl:]
		}
	}
	if len(b) < 8 || b[0] != 0 || b[1] != 0 {
		return false
	}
	return uint16(b[4])<<8|uint16(b[5]) == id && uint16(b[6])<<8|uint16(b[7]) == seq
}

func icmpChecksum(b []byte) uint16 {
	var s uint32
	for i := 0; i+1 < len(b); i += 2 {
		s += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		s += uint32(b[len(b)-1]) << 8
	}
	for s>>16 != 0 {
		s = s&0xffff + s>>16
	}
	return ^uint16(s)
}

// NetProbes is the DataplaneEnv of a real machine: NetDialTCP,
// NetDialViaProxy and NetPing. Platform Envs use it only when their
// constructor is given it (the helper, the app, the CLI), so tests that
// run every check on a platform Env with faked commands never dial out.
type NetProbes struct{}

var _ DataplaneEnv = NetProbes{}

func (NetProbes) DialTCP(ctx context.Context, addr string) error { return NetDialTCP(ctx, addr) }
func (NetProbes) DialViaProxy(ctx context.Context, proxy, addr, serverName string) error {
	return NetDialViaProxy(ctx, proxy, addr, serverName)
}
func (NetProbes) Ping(ctx context.Context, ip string) error { return NetPing(ctx, ip) }
