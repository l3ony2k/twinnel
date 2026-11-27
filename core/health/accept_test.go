package health

import (
	"reflect"
	"testing"
)

func acceptReport() *Report {
	return &Report{Overall: StatusFail, Checks: []CheckReport{
		{ID: "vpn-conflict", Result: Result{Status: StatusFail, Causes: []string{CauseExitNode}}},
		{ID: "tailnet-route", Result: Result{Status: StatusFail}},
		{ID: "magicdns", Result: Result{Status: StatusOK}},
		{ID: "system-dns", Result: Result{Status: StatusWarn}},
	}}
}

func TestAcceptExitNode(t *testing.T) {
	raw := acceptReport()
	got := Accept(raw, CauseExitNode)
	if got == raw {
		t.Fatal("Accept returned the raw report")
	}
	want := map[string]Status{"vpn-conflict": StatusAccepted, "tailnet-route": StatusAccepted, "magicdns": StatusOK, "system-dns": StatusWarn}
	for _, cr := range got.Checks {
		if cr.Status != want[cr.ID] {
			t.Errorf("%s: %s, want %s", cr.ID, cr.Status, want[cr.ID])
		}
	}
	if got.Checks[0].Was != StatusFail || got.Checks[1].CausedBy != "vpn-conflict" || got.Checks[1].Was != StatusFail {
		t.Errorf("Was/CausedBy: %+v %+v", got.Checks[0], got.Checks[1])
	}
	if got.Overall != StatusWarn {
		t.Errorf("overall %s, want warn", got.Overall)
	}
	c := got.Counts()
	if c[StatusFail] != 0 || c[StatusWarn] != 1 {
		t.Errorf("counts %v", c)
	}
	if raw.Checks[0].Status != StatusFail || raw.Overall != StatusFail {
		t.Error("Accept changed the raw report")
	}
}

func TestAcceptNeedsEveryCause(t *testing.T) {
	raw := acceptReport()
	raw.Checks[0].Causes = []string{CauseExitNode, CauseOtherVPN}
	if got := Accept(raw, CauseExitNode); got != raw {
		t.Errorf("a check with an unaccepted cause was accepted: %+v", got.Checks[0])
	}
	if got := Accept(raw); got != raw {
		t.Error("nothing accepted must return r")
	}
	if Accept(nil, CauseExitNode) != nil {
		t.Error("nil report")
	}
}

func TestDedupeEvidence(t *testing.T) {
	got := DedupeEvidence([]string{"a", "b", "a", "a", "c"})
	want := []EvidenceLine{{"a", 3}, {"b", 1}, {"c", 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}
