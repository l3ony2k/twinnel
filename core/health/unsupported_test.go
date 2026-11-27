package health

import (
	"context"
	"fmt"
	"testing"
)

// noEgress is an Env that cannot run the egress test.
type noEgress struct{ Env }

func wantUnsupported(t *testing.T, r Result, what string) {
	t.Helper()
	if r.Status != StatusSkip || !r.Unsupported || r.Fix != nil || len(r.Reasons) > 0 {
		t.Fatalf("%s: %+v, want an unsupported skip without a fix", what, r)
	}
}

// An egress test that cannot run here is a plain skip, whether or not the
// user turned periodic testing on: never a warning, and no 检测出口 offer.
func TestEgressUnsupportedIsASkip(t *testing.T) {
	for _, on := range []bool{true, false} {
		env := healthyEnv()
		env.platform = Windows
		env.settings.Egress = on
		r := LeakEgress{}.Run(context.Background(), noEgress{env})
		wantUnsupported(t, r, fmt.Sprint("egress on=", on))
		if r.Summary != "在 Windows 上做不了这项检查" {
			t.Errorf("summary %q", r.Summary)
		}
		// Through the runner (Memo wraps the Env) too.
		rep := (&Runner{Registry: DefaultRegistry(), Only: []string{"leak-guard-egress"}}).Run(context.Background(), noEgress{env})
		wantUnsupported(t, rep.Checks[0].Result, "runner")
		if rep.Overall != StatusOK {
			t.Errorf("overall %s: an unsupported check counted as a problem", rep.Overall)
		}
	}
}

// A request that turns out unsupported at run time (no way to bind the
// socket on this OS) is the same skip, with what was seen as evidence.
func TestEgressUnsupportedAtRunTime(t *testing.T) {
	env := healthyEnv()
	env.platform = Linux
	env.egressErr = fmt.Errorf("bind: %w", ErrNotSupported)
	// STUN gives a baseline; the proxied request is unsupported too.
	env.ts.Self.Addrs = []string{"203.0.113.50:41641"}
	r := LeakEgress{}.Run(context.Background(), env)
	wantUnsupported(t, r, "proxied unsupported")
	if len(r.Evidence) == 0 {
		t.Error("evidence dropped")
	}
	// No baseline at all.
	env.ts.Self.Addrs = nil
	wantUnsupported(t, LeakEgress{}.Run(context.Background(), env), "direct unsupported")
}

func TestNotOnPlatformIsUnsupported(t *testing.T) {
	reg := NewRegistry()
	reg.Register(onlyLinux{})
	env := healthyEnv()
	env.platform = Windows
	rep := (&Runner{Registry: reg}).Run(context.Background(), env)
	wantUnsupported(t, rep.Checks[0].Result, "not on platform")
	if rep.Checks[0].Summary != "在 Windows 上做不了这项检查" {
		t.Errorf("summary %q", rep.Checks[0].Summary)
	}
}

type onlyLinux struct{}

func (onlyLinux) ID() string                      { return "linux-only-x" }
func (onlyLinux) Title() string                   { return "x" }
func (onlyLinux) Platforms() []string             { return []string{Linux} }
func (onlyLinux) Run(context.Context, Env) Result { return Result{Status: StatusFail} }

// unreadable fails the facts a platform cannot read with ErrNotSupported.
type unreadable struct{ *fakeEnv }

func (unreadable) Interfaces(context.Context) ([]Interface, error) {
	return nil, fmt.Errorf("adapters: %w", ErrNotSupported)
}
func (unreadable) NetworkServices(context.Context) ([]NetworkService, error) {
	return nil, fmt.Errorf("services: %w", ErrNotSupported)
}
func (unreadable) VPNs(context.Context) ([]VPN, error) {
	return nil, fmt.Errorf("vpns: %w", ErrNotSupported)
}

// A fact the platform cannot read makes the check an unsupported skip, not
// a read failure (which is a problem the user is asked to fix).
func TestUnreadableFactsAreUnsupported(t *testing.T) {
	env := unreadable{healthyEnv()}
	wantUnsupported(t, TailnetRoute{}.Run(context.Background(), env), "tailnet-route")
	wantUnsupported(t, SystemDNS{}.Run(context.Background(), env), "system-dns")
	r := VPNConflict{}.Run(context.Background(), env)
	for _, rs := range r.Reasons {
		if rs == ReasonReadFailed {
			t.Errorf("vpn-conflict: read-failed for an unsupported VPN list: %+v", r)
		}
	}
}

func TestPlatformName(t *testing.T) {
	for in, want := range map[string]string{Darwin: "macOS", Linux: "Linux", Windows: "Windows", "plan9": "plan9"} {
		if got := PlatformName(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
