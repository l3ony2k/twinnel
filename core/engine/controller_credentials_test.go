package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

func TestTakeoverPlanCandidates(t *testing.T) {
	h := newAdoptHarness(t)
	p := h.plan()
	chosen := 0
	for _, c := range p.Candidates {
		if c.Chosen {
			chosen++
			if c.Path != p.Base || !c.Explains {
				t.Fatalf("chosen candidate %+v, base %s", c, p.Base)
			}
		}
	}
	if chosen != 1 {
		t.Fatalf("candidates %+v", p.Candidates)
	}
	// The stored plan (list and single) carries them too.
	got, err := h.e.TakeoverGet(p.PlanID)
	if err != nil || len(got.Candidates) != len(p.Candidates) {
		t.Fatalf("get %+v %v", got.Candidates, err)
	}
}

func TestControllerCredentials(t *testing.T) {
	h := newAdoptHarness(t)
	ctx := context.Background()
	p := h.plan()
	if _, err := h.e.Adopt(ctx, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash}); err != nil {
		t.Fatal(err)
	}
	c, err := h.e.ControllerCredentials()
	if err != nil || c.Secret != "" || c.Source != rpc.CredentialsFromConfig || c.Controller == "" {
		t.Fatalf("before secure %v %v", c, err)
	}
	if _, err := h.e.SecureController(ctx); err != nil {
		t.Fatal(err)
	}
	stored, _ := h.sec.Get(store.SecretController)
	c, err = h.e.ControllerCredentials()
	if err != nil || stored == "" || c.Secret != stored {
		t.Fatalf("after secure: credentials do not carry the new secret (%v)", err)
	}
	for _, s := range []string{c.String(), fmt.Sprintf("%v %+v %#v", c, c, c)} {
		if strings.Contains(s, stored) {
			t.Fatalf("secret printed: %s", s)
		}
	}
}
