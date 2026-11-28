package rpc

import (
	"strings"
	"testing"
)

const testUserSID = "S-1-5-21-1000000001-2000000002-3000000003-1001"

func TestPipeSDDL(t *testing.T) {
	// The installed helper: LocalSystem serves, the installing user is a
	// client and can never create an instance (squat the pipe).
	got, err := PipeSDDL(SIDLocalSystem, testUserSID, "", SIDLocalSystem, testUserSID)
	if err != nil {
		t.Fatal(err)
	}
	want := "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x0012019b;;;" + testUserSID + ")"
	if got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	// A development helper, not elevated, serves as testUserSID: it needs
	// FILE_CREATE_PIPE_INSTANCE for every instance after the first, or its
	// second Accept fails with "Access is denied" (ui-review 8.2). Other
	// allowed users stay clients.
	const other = "S-1-5-21-1000000001-2000000002-3000000003-1002"
	got, err = PipeSDDL(testUserSID, other, testUserSID)
	if err != nil {
		t.Fatal(err)
	}
	want = "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x0012019f;;;" + testUserSID + ")(A;;0x0012019b;;;" + other + ")"
	if got != want {
		t.Fatalf("dev helper: got %s\nwant %s", got, want)
	}
	if PipeServerAccess&^PipeClientAccess != 0x4 || PipeClientAccess&0x4 != 0 {
		t.Errorf("server access %#x adds more than FILE_CREATE_PIPE_INSTANCE to %#x", PipeServerAccess, PipeClientAccess)
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
