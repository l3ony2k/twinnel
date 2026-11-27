package tailscale

import (
	"context"
	"errors"
	"testing"

	"tailscale.com/ipn"
)

type loginAPI struct {
	fakeAPI
	logins int
}

func (l *loginAPI) StartLoginInteractive(context.Context) error { l.logins++; return nil }

func TestLoginInSession(t *testing.T) {
	for _, unattended := range []bool{true, false} {
		api := &loginAPI{fakeAPI: fakeAPI{prefs: &ipn.Prefs{}}}
		if err := New(api, true).LoginInSession(context.Background(), unattended); err != nil {
			t.Fatal(err)
		}
		e := api.edits[0]
		if len(api.edits) != 1 || !e.WantRunningSet || !e.WantRunning || e.ForceDaemonSet != unattended || e.ForceDaemon != unattended || api.logins != 1 {
			t.Fatalf("unattended=%v: edits %+v logins %d", unattended, api.edits, api.logins)
		}
	}
	if err := New(&loginAPI{fakeAPI: fakeAPI{prefs: &ipn.Prefs{}}}, false).LoginInSession(context.Background(), true); !errors.Is(err, ErrWritesDisabled) {
		t.Fatalf("read-only: %v", err)
	}
}
