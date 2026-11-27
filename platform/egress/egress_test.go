package egress

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/health"
)

func TestBindOption(t *testing.T) {
	ifc := Iface{Index: 7, Name: "en0"}
	for _, tc := range []struct {
		goos, network string
		want          SockOpt
	}{
		{"darwin", "tcp4", SockOpt{Level: 0, Name: 25, Int: 7}},   // IP_BOUND_IF
		{"darwin", "tcp6", SockOpt{Level: 41, Name: 125, Int: 7}}, // IPV6_BOUND_IF
		{"linux", "tcp4", SockOpt{Level: 1, Name: 25, Str: "en0", IsStr: true}},
		{"linux", "tcp6", SockOpt{Level: 1, Name: 25, Str: "en0", IsStr: true}},
		{"windows", "tcp6", SockOpt{Level: 41, Name: 31, Int: 7}}, // IPV6_UNICAST_IF, host order
	} {
		got, err := BindOption(tc.goos, tc.network, ifc)
		if err != nil || got != tc.want {
			t.Errorf("%s %s: %+v %v, want %+v", tc.goos, tc.network, got, err, tc.want)
		}
	}
	if _, err := BindOption("plan9", "tcp4", ifc); !errors.Is(err, health.ErrNotSupported) {
		t.Errorf("plan9: %v", err)
	}
	if _, err := BindOption("windows", "tcp4", Iface{Name: "Wi-Fi"}); err == nil {
		t.Error("windows without an index: no error")
	}
	if _, err := BindOption("linux", "tcp4", Iface{Index: 3}); err == nil {
		t.Error("linux without a name: no error")
	}
}

// IP_UNICAST_IF reads the index in network byte order: the four bytes of
// the int32 setsockopt passes must be the big-endian index, whatever the
// host's byte order (an index above 127 sets the sign bit, too).
func TestBindOptionWindowsIPv4NetworkOrder(t *testing.T) {
	for _, idx := range []int{1, 7, 43, 200, 70000} {
		o, err := BindOption("windows", "tcp4", Iface{Index: idx, Name: "Wi-Fi"})
		if err != nil || o.Level != 0 || o.Name != 31 {
			t.Fatalf("%d: %+v %v", idx, o, err)
		}
		var got, want [4]byte
		binary.NativeEndian.PutUint32(got[:], uint32(int32(o.Int)))
		binary.BigEndian.PutUint32(want[:], uint32(idx))
		if got != want {
			t.Errorf("index %d: bytes %v, want %v", idx, got, want)
		}
	}
}

func TestDestFor(t *testing.T) {
	if a := DestFor(health.DefaultEgressURLs[0]); a.String() != "1.1.1.1" {
		t.Error(a)
	}
	if a := DestFor("https://[2001:db8::1]/trace"); a.String() != "2001:db8::1" {
		t.Error(a)
	}
	if a := DestFor("https://echo.example/trace"); a.String() != "1.1.1.1" {
		t.Error(a)
	}
}

// echoServer answers like Cloudflare's trace endpoint, with a synthetic IP.
func echoServer(t *testing.T) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "fl=1\nip=203.0.113.9\nts=1\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func loopback(t *testing.T) Iface {
	t.Helper()
	ifs, err := net.Interfaces()
	if err != nil {
		t.Skip(err)
	}
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback != 0 && i.Flags&net.FlagUp != 0 {
			return Iface{Index: i.Index, Name: i.Name}
		}
	}
	t.Skip("no loopback interface")
	return Iface{}
}

// The dialer really sets the option on this OS: pinned to the loopback
// interface it reaches a server on 127.0.0.1; pinned to an interface that
// does not exist it fails at the socket option, not later.
func TestDialerPinsTheSocket(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("runs where the tests run: darwin and linux")
	}
	srv := echoServer(t)
	lo := loopback(t)
	tlsConf := srv.Client().Transport.(*http.Transport).TLSClientConfig
	ip, err := fetch(context.Background(), srv.URL, Dialer(lo).DialContext, tlsConf)
	if err != nil || ip.String() != "203.0.113.9" {
		t.Fatalf("bound to %s: %v %v", lo.Name, ip, err)
	}
	_, err = fetch(context.Background(), srv.URL, Dialer(Iface{Index: 9999, Name: "twinnel-none0"}).DialContext, tlsConf)
	if err == nil || !strings.Contains(err.Error(), "bind to interface") {
		t.Fatalf("bound to a missing interface: %v", err)
	}
}

// fakeMixedPort is mihomo's mixed port for one CONNECT: it answers 200 and
// splices the tunnel to the echo server. It reports the client's source
// port, which mihomo lists as metadata.sourcePort.
func fakeMixedPort(t *testing.T, upstream string) (port int, src chan int) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	src = make(chan int, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		src <- c.RemoteAddr().(*net.TCPAddr).Port
		br := bufio.NewReader(c)
		req, err := http.ReadRequest(br)
		if err != nil || req.Method != http.MethodConnect {
			return
		}
		up, err := net.Dial("tcp", upstream)
		if err != nil {
			fmt.Fprint(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return
		}
		defer up.Close()
		fmt.Fprint(c, "HTTP/1.1 200 Connection established\r\n\r\n")
		go io.Copy(up, br)
		io.Copy(c, up)
	}()
	return ln.Addr().(*net.TCPAddr).Port, src
}

func TestProxied(t *testing.T) {
	srv := echoServer(t)
	port, src := fakeMixedPort(t, srv.Listener.Addr().String())
	tlsConf := srv.Client().Transport.(*http.Transport).TLSClientConfig
	var asked int
	chain := func(_ context.Context, p int) ([]string, error) {
		asked = p
		return []string{"node-a", "PROXY"}, nil
	}
	ip, ch, err := proxied(context.Background(), srv.URL, port, chain, tlsConf)
	if err != nil || ip.String() != "203.0.113.9" || len(ch) != 2 || ch[0] != "node-a" {
		t.Fatalf("%v %v %v", ip, ch, err)
	}
	if want := <-src; asked != want {
		t.Errorf("chain looked up for source port %d, the tunnel's is %d", asked, want)
	}
}

func TestProxiedErrors(t *testing.T) {
	ctx := context.Background()
	none := func(context.Context, int) ([]string, error) { return nil, nil }
	if _, _, err := Proxied(ctx, "http://1.1.1.1/", 7890, none); err == nil {
		t.Error("http URL accepted")
	}
	if _, _, err := Proxied(ctx, health.DefaultEgressURLs[0], 0, none); err == nil {
		t.Error("port 0 accepted")
	}
}

func TestControllerChain(t *testing.T) {
	calls := 0
	get := func(_ context.Context, path string) ([]byte, error) {
		calls++
		if path != "/connections" {
			t.Errorf("path %s", path)
		}
		if calls < 2 {
			return []byte(`{"connections":[]}`), nil
		}
		return []byte(`{"connections":[{"metadata":{"sourcePort":"6123"},"chains":["node-x","PROXY","Final"]}]}`), nil
	}
	ch, err := ControllerChain(get)(context.Background(), 6123)
	if err != nil || len(ch) != 3 || ch[0] != "node-x" || calls != 2 {
		t.Fatalf("%v %v calls=%d", ch, err, calls)
	}
	_, err = ControllerChain(func(context.Context, string) ([]byte, error) { return nil, errors.New("down") })(context.Background(), 1)
	if err == nil {
		t.Error("controller error lost")
	}
}

func TestParseConnectionChain(t *testing.T) {
	body := []byte(`{"connections":[{"metadata":{"sourcePort":"5000"},"chains":["a","G"]},{"metadata":{"sourcePort":` + strconv.Itoa(6123) + `},"chains":["node-x","PROXY","Final"]}]}`)
	c, ok := ParseConnectionChain(body, 6123)
	if !ok || len(c) != 3 || c[0] != "node-x" {
		t.Fatal(c, ok)
	}
	if _, ok := ParseConnectionChain(body, 7); ok {
		t.Fatal("unexpected match")
	}
}
