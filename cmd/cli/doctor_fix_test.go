package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/healthfix"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// fixHelper fakes the helper for doctor --fix: system-dns fails until
// dns.point.
type fixHelper struct{ pointed bool }

func (h *fixHelper) Call(_ context.Context, method string, _, result any) error {
	var v any
	switch method {
	case rpc.MethodDNSStatus:
		v = rpc.DNSStatus{Supported: true, Target: []string{"127.0.0.1"}, Services: []rpc.DNSService{{Name: "Wi-Fi", Servers: []string{"192.0.2.1"}}}}
	case rpc.MethodDNSPoint:
		h.pointed = true
		v = rpc.DNSStatus{}
	case rpc.MethodHealthRun:
		cr := health.CheckReport{ID: "system-dns", Title: "系统 DNS", Result: health.Result{Status: health.StatusFail, Summary: "bad",
			Reasons: []string{health.ReasonDNSNotMihomo}}}
		if h.pointed {
			cr.Result = health.Result{Status: health.StatusOK, Summary: "good"}
		}
		b, _ := json.Marshal(health.Report{Checks: []health.CheckReport{cr}})
		v = rpc.HealthReport{Report: b}
	default:
		return nil
	}
	if result != nil {
		b, _ := json.Marshal(v)
		return json.Unmarshal(b, result)
	}
	return nil
}

func TestDoctorFix(t *testing.T) {
	for _, answer := range []string{"n\n", "y\n"} {
		h := &fixHelper{}
		var out bytes.Buffer
		err := runDoctorFix(fixOptions{check: "system-dns", in: strings.NewReader(answer), out: &out,
			fixer: func(context.Context) (*healthfix.Fixer, func(), error) {
				return &healthfix.Fixer{Helper: h, Lang: "en", Platform: health.Darwin}, func() {}, nil
			}})
		if err != nil {
			t.Fatal(err)
		}
		s := out.String()
		if !strings.Contains(s, "Wi-Fi: 192.0.2.1 → 127.0.0.1") {
			t.Errorf("no preview of the change:\n%s", s)
		}
		if answer == "n\n" && (h.pointed || !strings.Contains(s, "skipped")) {
			t.Errorf("applied without a yes:\n%s", s)
		}
		if answer == "y\n" && (!h.pointed || !strings.Contains(s, "now: ok") || !strings.Contains(s, "twinnel doctor --undo")) {
			t.Errorf("not applied, re-checked and undoable:\n%s", s)
		}
	}
}

// fixHelper restores the DNS on dns.restore.
func (h *fixHelper) restore() { h.pointed = false }

// doctor --undo says what the undo does and that the check fails again,
// and asks before it runs.
func TestDoctorUndoWarns(t *testing.T) {
	for _, answer := range []string{"n\n", "y\n"} {
		h := &undoHelper{fixHelper{pointed: true}}
		var out bytes.Buffer
		err := runDoctorFix(fixOptions{undo: `{"action":"system-dns","check":"system-dns","reason":"` + health.ReasonDNSNotMihomo + `","dns":true}`,
			in: strings.NewReader(answer), out: &out,
			fixer: func(context.Context) (*healthfix.Fixer, func(), error) {
				return &healthfix.Fixer{Helper: h, Lang: "en", Platform: health.Darwin}, func() {}, nil
			}})
		if err != nil {
			t.Fatal(err)
		}
		s := out.String()
		if !strings.Contains(s, "comes back") || !strings.Contains(s, "undo? [y/N]") {
			t.Errorf("no warning before the undo:\n%s", s)
		}
		if (answer == "y\n") == h.pointed {
			t.Errorf("answer %q: pointed %v\n%s", answer, h.pointed, s)
		}
	}
}

type undoHelper struct{ fixHelper }

func (h *undoHelper) Call(ctx context.Context, method string, p, result any) error {
	if method == rpc.MethodDNSRestore {
		h.restore()
		return nil
	}
	return h.fixHelper.Call(ctx, method, p, result)
}

// The printed undo is one single-quoted word in sh and PowerShell even
// when a layer's content has a quote.
func TestUndoArgQuotes(t *testing.T) {
	a := undoArg(&healthfix.Undo{Action: "x", Layers: []healthfix.LayerBefore{{ID: "l", Content: "name: 'it''s'\n"}}})
	if strings.Contains(a, "'") {
		t.Fatalf("quote in %s", a)
	}
	var u healthfix.Undo
	if err := json.Unmarshal([]byte(a), &u); err != nil || u.Layers[0].Content != "name: 'it''s'\n" {
		t.Fatalf("%+v %v", u, err)
	}
}
