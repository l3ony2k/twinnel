//go:build darwin

package darwin

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
)

var _ health.EgressEnv = (*Env)(nil)

// Socket options that pin a socket to an interface (netinet/in.h).
const (
	ipBoundIf   = 25  // IP_BOUND_IF
	ipv6BoundIf = 125 // IPV6_BOUND_IF
)

// physicalInterface is the first active network service's device with a
// global address, in service order (what mihomo's auto-detect-interface
// picks as well).
func (e *Env) physicalInterface(ctx context.Context) (string, error) {
	svcs, err := e.NetworkServices(ctx)
	if err != nil {
		return "", err
	}
	ifs, err := e.Interfaces(ctx)
	if err != nil {
		return "", err
	}
	byName := map[string]health.Interface{}
	for _, i := range ifs {
		byName[i.Name] = i
	}
	for _, s := range svcs {
		i, ok := byName[s.Device]
		if s.Device == "" || !ok || !i.Active || strings.HasPrefix(s.Device, "utun") {
			continue
		}
		for _, a := range i.Addrs {
			if a.Is4() && !a.IsLinkLocalUnicast() && !a.IsLoopback() {
				return s.Device, nil
			}
		}
	}
	return "", errors.New("no active physical interface")
}

// DirectEgress fetches url on a socket bound to the physical interface, so
// the request bypasses mihomo's TUN, and returns the IP the echo reports.
func (e *Env) DirectEgress(ctx context.Context, rawURL string) (netip.Addr, error) {
	name, err := e.physicalInterface(ctx)
	if err != nil {
		return netip.Addr{}, err
	}
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return netip.Addr{}, err
	}
	d := &net.Dialer{Timeout: 5 * time.Second, Control: func(network, _ string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			if strings.HasSuffix(network, "6") {
				serr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, ipv6BoundIf, ifi.Index)
			} else {
				serr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, ipBoundIf, ifi.Index)
			}
		})
		if err != nil {
			return err
		}
		return serr
	}}
	client := &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{
		Proxy: nil, DialContext: d.DialContext, ForceAttemptHTTP2: false,
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return netip.Addr{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return netip.Addr{}, err
	}
	defer resp.Body.Close()
	return readEgress(resp)
}

func readEgress(resp *http.Response) (netip.Addr, error) {
	if resp.StatusCode != http.StatusOK {
		return netip.Addr{}, fmt.Errorf("echo: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return netip.Addr{}, err
	}
	return health.ParseEgressBody(string(b))
}

// ProxiedEgress fetches url through mihomo's mixed (or HTTP) port with a
// CONNECT tunnel, looks up the tunnel in GET /connections to learn the
// proxy chain, and returns the IP the echo reports.
func (e *Env) ProxiedEgress(ctx context.Context, rawURL string) (netip.Addr, []string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return netip.Addr{}, nil, fmt.Errorf("egress URL must be https: %q", rawURL)
	}
	rt, err := e.MihomoRuntime(ctx)
	if err != nil {
		return netip.Addr{}, nil, err
	}
	port := rt.Configs.MixedPort
	if port == 0 {
		port = rt.Configs.Port
	}
	if port == 0 {
		return netip.Addr{}, nil, errors.New("mihomo has no mixed-port or port")
	}
	host := u.Hostname()
	target := net.JoinHostPort(host, portOr(u.Port(), "443"))
	d := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return netip.Addr{}, nil, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	}
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return netip.Addr{}, nil, fmt.Errorf("CONNECT via mihomo: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return netip.Addr{}, nil, fmt.Errorf("CONNECT via mihomo: %s", resp.Status)
	}
	// mihomo may wait for the first bytes (sniffing) before it dials and
	// lists the connection, so shake hands first, then look it up by our
	// source port while the tunnel is still open.
	tc := tls.Client(&bufConn{Conn: conn, r: br}, &tls.Config{ServerName: host})
	if err := tc.HandshakeContext(ctx); err != nil {
		return netip.Addr{}, nil, fmt.Errorf("TLS via mihomo: %w", err)
	}
	local := conn.LocalAddr().(*net.TCPAddr).Port
	addr, secret := e.controller(ctx)
	chain, cerr := e.connectionChain(ctx, addr, secret, local)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return netip.Addr{}, nil, err
	}
	req.Header.Set("Connection", "close")
	if err := req.Write(tc); err != nil {
		return netip.Addr{}, nil, err
	}
	hr, err := http.ReadResponse(bufio.NewReader(tc), req)
	if err != nil {
		return netip.Addr{}, nil, err
	}
	defer hr.Body.Close()
	ip, err := readEgress(hr)
	if err != nil {
		return netip.Addr{}, nil, err
	}
	if cerr != nil {
		return ip, nil, fmt.Errorf("proxy chain: %w", cerr)
	}
	return ip, chain, nil
}

func portOr(p, def string) string {
	if p == "" {
		return def
	}
	return p
}

// connectionChain finds the connection with the given source port in GET
// /connections and returns its chain (leaf first, as mihomo reports it).
func (e *Env) connectionChain(ctx context.Context, addr, secret string, srcPort int) ([]string, error) {
	for try := 0; try < 5; try++ {
		b, err := e.get(ctx, addr, secret, "/connections")
		if err != nil {
			return nil, err
		}
		if chain, ok := ParseConnectionChain(b, srcPort); ok {
			return chain, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, errors.New("tunnel not found in /connections")
}

// ParseConnectionChain finds the chain of the connection from srcPort in a
// GET /connections body.
func ParseConnectionChain(body []byte, srcPort int) ([]string, bool) {
	var v struct {
		Connections []struct {
			Metadata struct {
				SourcePort json.RawMessage `json:"sourcePort"`
			} `json:"metadata"`
			Chains []string `json:"chains"`
		} `json:"connections"`
	}
	if json.Unmarshal(body, &v) != nil {
		return nil, false
	}
	want := strconv.Itoa(srcPort)
	for _, c := range v.Connections {
		if strings.Trim(string(c.Metadata.SourcePort), `"`) == want {
			return c.Chains, true
		}
	}
	return nil, false
}

// bufConn reads first from the buffered reader that consumed the CONNECT
// response, then from the connection.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }
