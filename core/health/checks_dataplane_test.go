package health

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
)

func (f *fakeEnv) DialTCP(_ context.Context, addr string) error {
	f.dialed = append(f.dialed, addr)
	return f.dialErr
}
func (f *fakeEnv) DialViaProxy(_ context.Context, proxy, addr, sni string) error {
	f.proxied = append(f.proxied, proxy+"→"+addr+"#"+sni)
	return f.proxyErr
}
func (f *fakeEnv) Ping(context.Context, string) error { return f.pingErr }

// timeoutErr is a net.Error that timed out, as a dial past its deadline.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func init() {
	reasonCases[ReasonDataplaneTCP] = reasonCase{TUNDataplane{}, func() Env {
		e := healthyEnv()
		e.rt.Configs.TUN.Stack = "mixed"
		e.dialErr = timeoutErr{}
		return e
	}}
	reasonCases[ReasonDataplaneGvisor] = reasonCase{TUNDataplane{}, func() Env {
		e := healthyEnv()
		e.rt.Configs.TUN.Stack = "gvisor"
		e.dialErr = timeoutErr{}
		return e
	}}
	reasonCases[ReasonDataplaneDial] = reasonCase{TUNDataplane{}, func() Env {
		e := healthyEnv()
		e.dialErr = errors.New("network is unreachable")
		return e
	}}
}

func TestDataplaneOK(t *testing.T) {
	e := healthyEnv()
	r := want(t, TUNDataplane{}, e, StatusOK, "1.1.1.1:443")
	if len(r.Reasons) != 0 || r.Fix != nil {
		t.Fatalf("ok with reasons/fix: %+v", r)
	}
	if !slices.Equal(e.dialed, []string{"1.1.1.1:443"}) || len(e.proxied) != 0 {
		t.Fatalf("dialed %v, proxied %v: a passing round costs one handshake", e.dialed, e.proxied)
	}
	// A REJECT rule answers with a reset: still a working path.
	e = healthyEnv()
	e.dialErr = fmt.Errorf("%w: dial tcp: connect: connection refused", ErrRefused)
	want(t, TUNDataplane{}, e, StatusOK, zhDP.evTCPRefused)
}

func TestDataplaneDead(t *testing.T) {
	e := healthyEnv()
	e.rt.Configs.TUN.Stack = "Mixed"
	e.rt.Configs.MixedPort = 7899
	e.dialErr = timeoutErr{}
	r := want(t, TUNDataplane{}, e, StatusFail, "198.18.0.2")
	ev := strings.Join(r.Evidence, "\n")
	for _, s := range []string{"mixed", "127.0.0.1:7899", "1.1.1.1:443"} {
		if !strings.Contains(ev, s) {
			t.Errorf("evidence lacks %q:\n%s", s, ev)
		}
	}
	if r.Fix == nil || r.Fix.AutoFix != FixTUNStack || !slices.Equal(r.Reasons, []string{ReasonDataplaneTCP}) {
		t.Fatalf("fix %+v reasons %v", r.Fix, r.Reasons)
	}
	if !slices.Equal(e.proxied, []string{"127.0.0.1:7899→1.1.1.1:443#1.1.1.1"}) {
		t.Fatalf("proxied %v", e.proxied)
	}
	// A deadline from the context counts as no answer too.
	e = healthyEnv()
	e.dialErr = context.DeadlineExceeded
	e.pingErr = ErrNotSupported
	r = want(t, TUNDataplane{}, e, StatusFail, "")
	if strings.Contains(strings.Join(r.Evidence, "\n"), "ICMP") {
		t.Fatalf("unsupported ping shows up: %v", r.Evidence)
	}
}

func TestDataplaneSkips(t *testing.T) {
	e := healthyEnv()
	e.rt.Configs.TUN.Enable = false
	want(t, TUNDataplane{}, e, StatusSkip, zhDP.msgTUNOff)

	e = healthyEnv()
	e.rtErr, e.cfgErr = errors.New("down"), errors.New("none")
	want(t, TUNDataplane{}, e, StatusSkip, "none")

	// The probe IP goes elsewhere and fake-ip answers nothing.
	e = healthyEnv()
	e.routes["1.1.1.1"] = RouteLookup{Interface: "en0"}
	r := want(t, TUNDataplane{}, e, StatusSkip, "en0")
	if len(e.dialed) != 0 {
		t.Fatalf("dialed %v without a TUN route", e.dialed)
	}
	_ = r

	// An Env without probes.
	want(t, TUNDataplane{}, struct{ Env }{healthyEnv()}, StatusSkip, ErrNotSupported.Error())
}

// With the probe IP routed elsewhere (an exit node), the fake-ip of the
// probe name still goes into the TUN.
func TestDataplaneFakeIPFallback(t *testing.T) {
	e := healthyEnv()
	e.routes["1.1.1.1"] = RouteLookup{Interface: "utun9"}
	e.mihomo[DataplaneProbeName] = DNSAnswer{Rcode: "NOERROR", Addrs: []netip.Addr{a("198.18.3.4")}}
	e.routes["198.18.3.4"] = RouteLookup{Interface: "utun5"}
	e.dialErr = timeoutErr{}
	e.rt.Configs.MixedPort = 7899
	want(t, TUNDataplane{}, e, StatusFail, "198.18.3.4:443")
	if !slices.Equal(e.proxied, []string{"127.0.0.1:7899→198.18.3.4:443#" + DataplaneProbeName}) {
		t.Fatalf("proxied %v", e.proxied)
	}
}

func TestIsMihomoTUN(t *testing.T) {
	for _, c := range []struct {
		plat, iface, dev string
		want             bool
	}{
		{Darwin, "utun5", "utun5", true},
		{Darwin, "utun7", "utun", true},
		{Darwin, "utun7", "", true},
		{Darwin, "en0", "", false},
		{Windows, "Meta", "", true},
		{Windows, "Meta1", "", true},
		{Windows, "Wi-Fi", "", false},
		{Windows, "Mihomo", "Mihomo", true},
		{Linux, "Meta", "Meta", true},
		{Linux, "tailscale0", "", false},
		{Linux, "", "", false},
	} {
		if got := isMihomoTUN(c.plat, c.iface, TUNConfig{Device: c.dev}); got != c.want {
			t.Errorf("%s %q dev %q: %v", c.plat, c.iface, c.dev, got)
		}
	}
}

func TestTUNGateway(t *testing.T) {
	if g := tunGateway(TUNConfig{}); g != a("198.18.0.2") {
		t.Errorf("default gateway %s", g)
	}
	if g := tunGateway(TUNConfig{Inet4Address: []string{"172.19.0.1/30"}}); g != a("172.19.0.2") {
		t.Errorf("gateway %s", g)
	}
	if g := tunGateway(TUNConfig{Inet4Address: []string{"10.0.0.1/32"}}); g.IsValid() {
		t.Errorf("a /32 has no gateway, got %s", g)
	}
}

func TestIsTimeout(t *testing.T) {
	if !IsTimeout(timeoutErr{}) || !IsTimeout(fmt.Errorf("x: %w", context.DeadlineExceeded)) || IsTimeout(errors.New("x")) || IsTimeout(nil) {
		t.Fatal("IsTimeout")
	}
}

func TestDataplaneTextsComplete(t *testing.T) {
	ref := reflect.ValueOf(zhDP)
	v := reflect.ValueOf(enDP)
	for i := range ref.NumField() {
		name := ref.Type().Field(i).Name
		want, got := ref.Field(i).String(), v.Field(i).String()
		if got == "" || want == "" {
			t.Errorf("%s missing", name)
		}
		if a, b := verbRe.FindAllString(want, -1), verbRe.FindAllString(got, -1); !slices.Equal(a, b) {
			t.Errorf("%s: verbs %v vs %v", name, b, a)
		}
		for _, r := range got {
			if unicode.Is(unicode.Han, r) {
				t.Errorf("en %s has Chinese", name)
				break
			}
		}
	}
	if TitleOf("en", "tun-dataplane") != enDP.title || TitleOf("zh-Hans", "tun-dataplane") != zhDP.title {
		t.Fatal("titles")
	}
}

// The runner keeps the check inside its timeout even when every probe
// hangs until its own deadline.
func TestDataplaneBounded(t *testing.T) {
	if 2*DataplaneTimeout+time.Second >= 10*time.Second {
		t.Fatalf("worst case %s does not fit the runner's default timeout", 2*DataplaneTimeout+time.Second)
	}
}
