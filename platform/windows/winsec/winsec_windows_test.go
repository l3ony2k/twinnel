//go:build windows

package winsec

import (
	"testing"

	"github.com/l3ony2k/twinnel/core/store"
)

func TestDPAPIRoundTrip(t *testing.T) {
	s := Secrets(t.TempDir())
	if err := s.Set(store.SecretController, "synthetic"); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get(store.SecretController); err != nil || v != "synthetic" {
		t.Fatalf("%q %v", v, err)
	}
	sealed, _ := Protect([]byte("x"))
	sealed[len(sealed)-1] ^= 1
	if _, err := Unprotect(sealed); err == nil {
		t.Error("a damaged blob was opened")
	}
}
