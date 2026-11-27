package rpc

import (
	"strings"
	"testing"
)

const testUserSID = "S-1-5-21-1000000001-2000000002-3000000003-1001"

func TestPipeSDDL(t *testing.T) {
	got, err := PipeSDDL(testUserSID, "", SIDLocalSystem, testUserSID)
	if err != nil {
		t.Fatal(err)
	}
	want := "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x0012019b;;;" + testUserSID + ")"
	if got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	for _, bad := range []string{"S-1-5-21-1)(A;;GA;;;WD", "WD", "S-1-5", "s-1-5-18"} {
		if _, err := PipeSDDL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if s, _ := PipeSDDL(); strings.Contains(s, "WD") || !strings.HasPrefix(s, "D:P") {
		t.Errorf("default %s", s)
	}
}

func TestSIDAuthorizer(t *testing.T) {
	a := &SIDAuthorizer{SelfSID: "S-1-5-21-1-2-3-500", Allowed: []string{testUserSID}}
	for _, c := range []struct {
		p  Peer
		ok bool
	}{
		{Peer{SID: SIDLocalSystem}, true},
		{Peer{SID: "S-1-5-21-1-2-3-500"}, true},
		{Peer{SID: testUserSID}, true},
		{Peer{SID: "S-1-5-21-9-9-9-1002"}, false},
		{Peer{SID: "S-1-5-21-9-9-9-1002", Elevated: true}, false},             // elevated, not an admin
		{Peer{SID: "S-1-5-21-9-9-9-1002", Admin: true}, false},                // filtered token
		{Peer{SID: "S-1-5-21-9-9-9-1002", Elevated: true, Admin: true}, true}, // UAC-elevated admin
		{Peer{}, false},
	} {
		if err := a.Authorize(c.p); (err == nil) != c.ok {
			t.Errorf("%+v: err %v, want ok=%v", c.p, err, c.ok)
		}
	}
}
