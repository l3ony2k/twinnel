// Package egress runs the two requests of the egress test (health's
// leak-guard-egress) for every platform: one on a socket pinned to the
// physical interface, so it bypasses mihomo's TUN, and one through mihomo's
// mixed port.
//
// Pinning a socket to an interface is one socket option per platform:
//
//	macOS    IP_BOUND_IF / IPV6_BOUND_IF  (interface index)
//	Linux    SO_BINDTODEVICE              (interface name)
//	Windows  IP_UNICAST_IF                (index, network byte order)
//	         IPV6_UNICAST_IF              (index, host byte order)
//
// mihomo's own interface binding (component/dialer/bind_*.go) and
// Tailscale's netns package use the same options. Which option to set is
// decided by BindOption, a pure function tested on every OS; setting it
// (setsockopt_*.go) is the only OS-specific part.
package egress

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
)

// Socket option levels and names (the values each OS's headers define).
const (
	solSocketLinux     = 1  // SOL_SOCKET (Linux)
	soBindToDevice     = 25 // SO_BINDTODEVICE (Linux)
	ipprotoIP          = 0  // IPPROTO_IP
	ipprotoIPv6        = 41 // IPPROTO_IPV6
	ipBoundIfDarwin    = 25 // IP_BOUND_IF (netinet/in.h)
	ipv6BoundIfDarwin  = 125
	ipUnicastIfWindows = 31 // IP_UNICAST_IF (ws2ipdef.h)
	ipv6UnicastIfWin   = 31 // IPV6_UNICAST_IF
)

// Iface is the interface a socket is pinned to.
type Iface struct {
	Index int    // OS interface index
	Name  string // OS name (Linux: the device; Windows: the friendly name)
}

// SockOpt is one setsockopt call: an int value, or a string one (Str set).
type SockOpt struct {
	Level, Name int
	Int         int
	Str         string
	IsStr       bool
}

// BindOption returns the socket option that pins a socket of network
// ("tcp4", "tcp6", "udp4", ...) to ifc on goos.
func BindOption(goos, network string, ifc Iface) (SockOpt, error) {
	v6 := strings.HasSuffix(network, "6")
	switch goos {
	case health.Darwin:
		if ifc.Index <= 0 {
			return SockOpt{}, fmt.Errorf("bind to interface %q: no index", ifc.Name)
		}
		if v6 {
			return SockOpt{Level: ipprotoIPv6, Name: ipv6BoundIfDarwin, Int: ifc.Index}, nil
		}
		return SockOpt{Level: ipprotoIP, Name: ipBoundIfDarwin, Int: ifc.Index}, nil
	case health.Linux:
		if ifc.Name == "" {
			return SockOpt{}, errors.New("bind to interface: no name")
		}
		return SockOpt{Level: solSocketLinux, Name: soBindToDevice, Str: ifc.Name, IsStr: true}, nil
	case health.Windows:
		if ifc.Index <= 0 {
			return SockOpt{}, fmt.Errorf("bind to interface %q: no index", ifc.Name)
		}
		if v6 {
			return SockOpt{Level: ipprotoIPv6, Name: ipv6UnicastIfWin, Int: ifc.Index}, nil
		}
		// IP_UNICAST_IF takes the index in network byte order: the four
		// bytes setsockopt reads must be the big-endian index.
		return SockOpt{Level: ipprotoIP, Name: ipUnicastIfWindows, Int: int(windowsIPv4Index(uint32(ifc.Index)))}, nil
	}
	return SockOpt{}, fmt.Errorf("bind to interface on %s: %w", goos, health.ErrNotSupported)
}

// windowsIPv4Index is htonl(i) read back as a native uint32, so that the
// bytes of the int32 setsockopt passes are i in network order.
func windowsIPv4Index(i uint32) uint32 {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], i)
	return binary.NativeEndian.Uint32(b[:])
}

// Dialer returns a dialer whose sockets are pinned to ifc on this OS.
func Dialer(ifc Iface) *net.Dialer {
	return &net.Dialer{Timeout: 4 * time.Second, Control: func(network, _ string, c syscall.RawConn) error {
		o, err := BindOption(runtime.GOOS, network, ifc)
		if err != nil {
			return err
		}
		var serr error
		if err := c.Control(func(fd uintptr) { serr = setsockopt(fd, o) }); err != nil {
			return err
		}
		if serr != nil {
			return fmt.Errorf("bind to interface %s: %w", ifc.Name, serr)
		}
		return nil
	}}
}

// Direct GETs rawURL on sockets pinned to p.Iface (bypassing the TUN) and
// returns the IP the echo reports. A hostname is resolved over the same
// interface with p's DNS servers (Resolve), never with the system
// resolver, which may be mihomo's fake-ip DNS.
func Direct(ctx context.Context, rawURL string, p Path) (netip.Addr, error) {
	return direct(ctx, rawURL, p, Dialer(p.Iface), nil, nil)
}

func direct(ctx context.Context, rawURL string, p Path, d *net.Dialer, lookup LookupFunc, tlsConf *tls.Config) (netip.Addr, error) {
	host, port, err := hostPort(rawURL)
	if err != nil {
		return netip.Addr{}, err
	}
	addrs, err := Resolve(ctx, host, p, lookup)
	if err != nil {
		return netip.Addr{}, err
	}
	// Dial the resolved addresses in turn; the TLS name stays the URL's
	// host (http.Transport takes it from the request).
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		var errs []error
		for _, a := range addrs {
			c, err := d.DialContext(ctx, network, net.JoinHostPort(a.String(), port))
			if err == nil {
				return c, nil
			}
			errs = append(errs, err)
		}
		return nil, errors.Join(errs...)
	}
	return fetch(ctx, rawURL, dial, tlsConf)
}

// Fetch GETs rawURL with d (no proxy, no HTTP/2) and returns the IP the
// echo reports. d resolves names with the system resolver.
func Fetch(ctx context.Context, rawURL string, d *net.Dialer) (netip.Addr, error) {
	return fetch(ctx, rawURL, d.DialContext, nil)
}

func fetch(ctx context.Context, rawURL string, dial func(ctx context.Context, network, addr string) (net.Conn, error), tlsConf *tls.Config) (netip.Addr, error) {
	client := &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{
		Proxy: nil, DialContext: dial, ForceAttemptHTTP2: false, TLSClientConfig: tlsConf,
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

// DestFor is the address used to pick the physical route for rawURL: its
// host when that is an IP literal, else 1.1.1.1 (a hostname is resolved
// to IPv4 addresses, see Resolve).
func DestFor(rawURL string) netip.Addr {
	if u, err := url.Parse(rawURL); err == nil {
		if a, err := netip.ParseAddr(u.Hostname()); err == nil {
			return a.Unmap()
		}
	}
	return netip.MustParseAddr("1.1.1.1")
}

// ChainFunc returns the proxy chain of the mihomo connection whose source
// port is srcPort (GET /connections).
type ChainFunc func(ctx context.Context, srcPort int) ([]string, error)

// Proxied GETs rawURL through mihomo's mixed (or HTTP) port on 127.0.0.1
// with a CONNECT tunnel, looks the tunnel up with chain while it is open,
// and returns the IP the echo reports and the chain (leaf first).
func Proxied(ctx context.Context, rawURL string, port int, chain ChainFunc) (netip.Addr, []string, error) {
	return proxied(ctx, rawURL, port, chain, nil)
}

func proxied(ctx context.Context, rawURL string, port int, chain ChainFunc, tlsConf *tls.Config) (netip.Addr, []string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return netip.Addr{}, nil, fmt.Errorf("egress URL must be https: %q", rawURL)
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
	cfg := &tls.Config{ServerName: host}
	if tlsConf != nil {
		cfg = tlsConf.Clone()
		cfg.ServerName = host
	}
	tc := tls.Client(&bufConn{Conn: conn, r: br}, cfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		return netip.Addr{}, nil, fmt.Errorf("TLS via mihomo: %w", err)
	}
	local := conn.LocalAddr().(*net.TCPAddr).Port
	ch, cerr := chain(ctx, local)
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
	return ip, ch, nil
}

func portOr(p, def string) string {
	if p == "" {
		return def
	}
	return p
}

// GetFunc GETs a controller path and returns the body.
type GetFunc func(ctx context.Context, path string) ([]byte, error)

// ControllerChain is a ChainFunc over GET /connections: it retries a few
// times, since mihomo lists a connection only once it has dialed out.
func ControllerChain(get GetFunc) ChainFunc {
	return func(ctx context.Context, srcPort int) ([]string, error) {
		for try := 0; try < 5; try++ {
			b, err := get(ctx, "/connections")
			if err != nil {
				return nil, err
			}
			if chain, ok := ParseConnectionChain(b, srcPort); ok {
				return chain, nil
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
		return nil, errors.New("tunnel not found in /connections")
	}
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
