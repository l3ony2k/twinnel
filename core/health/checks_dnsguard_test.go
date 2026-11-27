package health

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// guarded is healthyEnv with the helper's DNS facts (synthetic).
func guarded(g DNSGuard) *fakeEnv {
	e := healthyEnv()
	e.settings.DNSGuard = func(context.Context) (DNSGuard, error) { return g, nil }
	return e
}

// stopped: mihomo is gone (no controller, no DNS answers).
func stopped(e *fakeEnv) *fakeEnv {
	e.rt, e.rtErr = nil, errors.New("connect: connection refused")
	e.mihomo = nil
	return e
}

func init() {
	reasonCases[ReasonDNSMihomoDown] = reasonCase{SystemDNS{}, func() Env { return stopped(guarded(DNSGuard{})) }}
	reasonCases[ReasonDNSRestored] = reasonCase{SystemDNS{}, func() Env {
		e := stopped(guarded(DNSGuard{Restored: []string{"Wi-Fi"}}))
		e.svcs[1].DNS = []string{"192.168.1.1"}
		return e
	}}
	reasonCases[ReasonDNSNoBackup] = reasonCase{SystemDNS{}, func() Env { return guarded(DNSGuard{Unprotected: []string{"Wi-Fi"}}) }}
}

func TestSystemDNSGuard(t *testing.T) {
	// Pointed and protected, mihomo up: ok.
	want(t, SystemDNS{}, guarded(DNSGuard{}), StatusOK, "")

	// D0: pointed before Twinnel, no backup: warn with the one-click
	// backup fix.
	r := want(t, SystemDNS{}, guarded(DNSGuard{Unprotected: []string{"Wi-Fi"}}), StatusWarn, "Wi-Fi")
	if !slices.Equal(r.Reasons, []string{ReasonDNSNoBackup}) || r.Fix == nil || r.Fix.AutoFix != FixDNSBackup {
		t.Fatalf("no backup: %+v", r)
	}
	if !strings.Contains(r.Summary, "没有可恢复的备份") {
		t.Fatalf("summary %q", r.Summary)
	}
	// A service not pointed at mihomo is not "unprotected", whatever the
	// helper says (names compared with the pointed ones).
	e := guarded(DNSGuard{Unprotected: []string{"Thunderbolt Bridge"}})
	want(t, SystemDNS{}, e, StatusOK, "")

	// Item 4: mihomo stopped while DNS points at it: fail, not a warning,
	// with restart-mihomo; with no backup both fixes are offered.
	r = want(t, SystemDNS{}, stopped(guarded(DNSGuard{Unprotected: []string{"Wi-Fi"}})), StatusFail, "Wi-Fi")
	if !slices.Equal(r.Reasons, []string{ReasonDNSMihomoDown, ReasonDNSNoBackup}) || r.Fix.AutoFix != FixRestartMihomo {
		t.Fatalf("down, no backup: %+v", r)
	}
	r = want(t, SystemDNS{}, stopped(guarded(DNSGuard{})), StatusFail, zhDNSGuard.evWatchdogWill)
	if !slices.Equal(r.Reasons, []string{ReasonDNSMihomoDown}) {
		t.Fatalf("down: %+v", r)
	}
	// Without the helper's facts (the app in-process) it is still a fail.
	e = stopped(healthyEnv())
	want(t, SystemDNS{}, e, StatusFail, "Wi-Fi")

	// A controller that refuses the secret, or a DNS that still answers,
	// is not "down".
	e = guarded(DNSGuard{})
	e.rt, e.rtErr = nil, errors.New("HTTP 401")
	e.mihomo = map[string]DNSAnswer{DataplaneProbeName: {Rcode: "NOERROR"}}
	want(t, SystemDNS{}, e, StatusOK, "")

	// The watchdog restored Wi-Fi while mihomo is down: fail (mihomo is
	// down), not "point the DNS" advice and not a hijack warning.
	e = stopped(guarded(DNSGuard{Restored: []string{"Wi-Fi"}}))
	e.svcs[1].DNS = []string{"192.0.2.53"}
	r = want(t, SystemDNS{}, e, StatusFail, zhDNSGuard.evRestored)
	if !slices.Equal(r.Reasons, []string{ReasonDNSRestored}) || r.Fix.AutoFix != FixRestartMihomo {
		t.Fatalf("restored: %+v", r)
	}
	// mihomo answers again, the point-back is pending: warn.
	e = guarded(DNSGuard{Restored: []string{"Wi-Fi"}})
	e.svcs[1].DNS = []string{"192.0.2.53"}
	r = want(t, SystemDNS{}, e, StatusWarn, "Wi-Fi")
	if !slices.Equal(r.Reasons, []string{ReasonDNSRestored}) {
		t.Fatalf("restored, back: %+v", r)
	}
}

// The D0 machine: Windows, Wi-Fi set to 127.0.0.1 by hand (no ::1). The
// check, dns.point and the watchdog all call that pointed (sysdns.PointsAt).
func TestSystemDNSWindowsV4Only(t *testing.T) {
	e := guarded(DNSGuard{Unprotected: []string{"Wi-Fi"}})
	e.platform = Windows
	e.cfg.TUN.DNSHijack, e.rt.Configs.TUN.DNSHijack = nil, nil
	e.svcs[1].DNS = []string{"127.0.0.1"}
	want(t, SystemDNS{}, e, StatusWarn, zhDNSGuard.evNoBackup)
}

func TestDNSGuardTextsComplete(t *testing.T) {
	ref, v := reflect.ValueOf(zhDNSGuard), reflect.ValueOf(enDNSGuard)
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
}
