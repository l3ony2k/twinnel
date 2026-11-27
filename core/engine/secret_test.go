package engine

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/store"
)

func TestGeneratesControllerSecret(t *testing.T) {
	h := newHarness(t)
	h.e.Secrets.Delete(store.SecretController)
	r := h.apply("fresh install")
	s, err := h.e.Secrets.Get(store.SecretController)
	if err != nil || len(s) != 48 {
		t.Fatalf("secret %q %v", s, err)
	}
	if !bytes.Contains(h.disk(), []byte("secret: "+s)) {
		t.Error("generated secret not in the written config")
	}
	if !slices.ContainsFunc(r.Warnings, func(w string) bool { return strings.HasPrefix(w, "generated a new controller secret") }) {
		t.Errorf("warnings %v", r.Warnings)
	}
	// Second apply keeps it.
	h.apply("again")
	if s2, _ := h.e.Secrets.Get(store.SecretController); s2 != s {
		t.Error("secret changed")
	}
}

func TestNoSecretForAdoptedConfig(t *testing.T) {
	h := newHarness(t)
	h.e.Secrets.Delete(store.SecretController)
	// A foreign config outside the state dir, without a secret.
	os.WriteFile(h.cfgPath, []byte("mixed-port: 7890\n"), 0o600)
	h.apply("adopted")
	if _, err := h.e.Secrets.Get(store.SecretController); err == nil {
		t.Error("secret generated for an adopted config")
	}
	if bytes.Contains(h.disk(), []byte("\nsecret:")) {
		t.Error("secret written")
	}
}
