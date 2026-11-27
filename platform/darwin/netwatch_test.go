//go:build darwin

package darwin

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/health/healthtest"
)

// fakeNet answers the fingerprint commands from testdata, with overrides.
type fakeNet struct {
	mu   sync.Mutex
	outs map[string]string // "route -n get default" etc. → output; "" = error
}

func newFakeNet(t *testing.T) *fakeNet {
	return &fakeNet{outs: map[string]string{
		"/sbin/route -n get default":        read(t, "route_get_default.txt"),
		"/sbin/route -n get -inet6 default": "",
		"/sbin/ifconfig":                    read(t, "ifconfig.txt"),
		"/usr/sbin/scutil --dns":            read(t, "scutil_dns.txt"),
	}}
}

func (f *fakeNet) set(cmd, out string) {
	f.mu.Lock()
	f.outs[cmd] = out
	f.mu.Unlock()
}

func (f *fakeNet) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.Join(append([]string{name}, args...), " ")
	out, ok := f.outs[key]
	if !ok {
		return nil, errors.New("unexpected command " + key)
	}
	if out == "" {
		return nil, errors.New("route: writing to routing socket: not in table")
	}
	return []byte(out), nil
}

func TestTakeFingerprint(t *testing.T) {
	f := newFakeNet(t)
	fp := TakeFingerprint(context.Background(), f.run)
	if !strings.HasPrefix(fp.Route, "utun5 via 198.18.0.1; none") {
		t.Errorf("route = %q", fp.Route)
	}
	for _, s := range []string{"en0 up=true active=true 192.168.1.20", "utun9 up=true active=true 100.101.0.6,fd7a:115c:a1e0::aa:bb"} {
		if !strings.Contains(fp.Interfaces, s) {
			t.Errorf("interfaces missing %q:\n%s", s, fp.Interfaces)
		}
	}
	for _, s := range []string{"utun2", "bridge0", "fe80"} { // only link-local or no address
		if strings.Contains(fp.Interfaces, s) {
			t.Errorf("interfaces contain %q:\n%s", s, fp.Interfaces)
		}
	}
	if !strings.Contains(fp.DNS, "ns=127.0.0.1") || !strings.Contains(fp.DNS, "ns=100.100.100.100") {
		t.Errorf("dns = %q", fp.DNS)
	}
	if again := TakeFingerprint(context.Background(), f.run); again != fp {
		t.Error("fingerprint not stable")
	}

	// A link-local-only change (awdl0 flapping) does not change it.
	f.set("/sbin/ifconfig", read(t, "ifconfig.txt")+
		"awdl0: flags=8943<UP,BROADCAST,RUNNING,PROMISC,SIMPLEX,MULTICAST> mtu 1500\n"+
		"\tinet6 fe80::1:2%awdl0 prefixlen 64 scopeid 0x11\n\tstatus: active\n")
	if TakeFingerprint(context.Background(), f.run) != fp {
		t.Error("awdl0 changed the fingerprint")
	}

	// Wi-Fi gets a new address and DNS: both parts change.
	f.set("/sbin/ifconfig", strings.Replace(read(t, "ifconfig.txt"), "192.168.1.20", "10.0.0.7", 1))
	f.set("/usr/sbin/scutil --dns", strings.Replace(read(t, "scutil_dns.txt"), "127.0.0.1", "10.0.0.1", 1))
	got := TakeFingerprint(context.Background(), f.run).Diff(fp)
	if strings.Join(got, ",") != "interfaces,dns" {
		t.Errorf("diff = %v", got)
	}
}

func TestNetWatcher(t *testing.T) {
	clk := healthtest.NewFakeClock()
	var mu sync.Mutex
	cur := NetFingerprint{Route: "en0", Interfaces: "a", DNS: "x"}
	takes := 0
	set := func(fp NetFingerprint) { mu.Lock(); cur = fp; mu.Unlock() }
	w := &NetWatcher{
		Poll:  3 * time.Second,
		Clock: clk,
		Take: func(context.Context) NetFingerprint {
			mu.Lock()
			defer mu.Unlock()
			takes++
			return cur
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Watch(ctx) }()

	takeCount := func() int { mu.Lock(); defer mu.Unlock(); return takes }
	// step advances one poll and waits until the watcher has re-armed.
	step := func() {
		t.Helper()
		healthtest.WaitUntil(t, "poll timer", func() bool { return clk.Armed(3 * time.Second) })
		n := takeCount()
		clk.Advance(3 * time.Second)
		healthtest.WaitUntil(t, "poll", func() bool { return takeCount() > n })
	}
	none := func() {
		t.Helper()
		select {
		case r := <-w.C():
			t.Fatalf("unexpected %q", r)
		case <-time.After(20 * time.Millisecond):
		}
	}
	want := func(reason string) {
		t.Helper()
		healthtest.WaitUntil(t, "re-arm", func() bool { return clk.Armed(3 * time.Second) })
		select {
		case r := <-w.C():
			if r != reason {
				t.Fatalf("reason = %q, want %q", r, reason)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no %q", reason)
		}
	}

	step() // unchanged
	none()

	// A blip that reverts before Settle is not reported.
	set(NetFingerprint{Route: "en0", Interfaces: "b", DNS: "x"})
	step()
	set(NetFingerprint{Route: "en0", Interfaces: "a", DNS: "x"})
	step()
	none()

	// A change that holds is reported once, after one more poll.
	set(NetFingerprint{Route: "en1", Interfaces: "a", DNS: "y"})
	step()
	none()
	step()
	want("network: route, dns")
	step()
	none()

	// A change that keeps moving is reported once it settles, against the
	// last reported state.
	set(NetFingerprint{Route: "en1", Interfaces: "c", DNS: "y"})
	step()
	set(NetFingerprint{Route: "en1", Interfaces: "d", DNS: "y"})
	step()
	none()
	step()
	want("network: interfaces")

	cancel()
	if err := <-done; err != context.Canceled {
		t.Errorf("Watch returned %v", err)
	}
}

// TWINNEL_REAL=1 go test -run Real ./platform/darwin/ (read-only commands).
func TestTakeFingerprintReal(t *testing.T) {
	if os.Getenv("TWINNEL_REAL") == "" {
		t.Skip("set TWINNEL_REAL=1")
	}
	run := ExecRunner(5 * time.Second)
	t0 := time.Now()
	a := TakeFingerprint(context.Background(), run)
	d := time.Since(t0)
	b := TakeFingerprint(context.Background(), run)
	t.Logf("took %v\nroute: %s\ninterfaces:\n%s\ndns lines: %d", d, a.Route, a.Interfaces, strings.Count(a.DNS, "\n")+1)
	if diff := a.Diff(b); len(diff) > 0 {
		t.Errorf("fingerprint changed between two immediate takes: %v", diff)
	}
}
