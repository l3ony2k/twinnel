//go:build darwin

package keychain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/store"
)

type call struct {
	stdin string
	args  []string
}

func fake(calls *[]call, code int, stdout string) Runner {
	return func(_ context.Context, stdin []byte, args ...string) ([]byte, []byte, int, error) {
		*calls = append(*calls, call{string(stdin), args})
		return []byte(stdout), []byte("boom"), code, nil
	}
}

func TestSecretNeverInArgv(t *testing.T) {
	var calls []call
	k := &Keychain{Path: "/tmp/x.keychain", Run: fake(&calls, 0, "")}
	secret := "https://sub.example/x?token=s3cr3t"
	if err := k.Set("subscription-url/main", secret); err != nil {
		t.Fatal(err)
	}
	c := calls[0]
	if strings.Join(c.args, " ") != "-i" {
		t.Errorf("argv %q", c.args)
	}
	for _, a := range c.args {
		if strings.Contains(a, "s3cr3t") {
			t.Error("secret in argv")
		}
	}
	if strings.Contains(c.stdin, "s3cr3t") {
		t.Error("secret in clear on stdin; want hex")
	}
	want := `add-generic-password -U -a "duet" -s "duet.subscription-url/main" -l "duet.subscription-url/main" -X 68747470733a2f2f7375622e6578616d706c652f783f746f6b656e3d733363723374 "/tmp/x.keychain"` + "\n"
	if c.stdin != want {
		t.Errorf("stdin\n got %q\nwant %q", c.stdin, want)
	}
}

func TestEncoding(t *testing.T) {
	for _, v := range []string{"", "abc", "deadbeef", "秘密", "a\nb", "duet-b64:x", "\xff"} {
		e := encode(v)
		if d, err := decode(e); err != nil || d != v {
			t.Errorf("%q -> %q -> %q %v", v, e, d, err)
		}
	}
	if encode("https://x/y?a=b") != "https://x/y?a=b" {
		t.Error("ASCII value encoded")
	}
}

func TestGetDelete(t *testing.T) {
	var calls []call
	k := &Keychain{Run: fake(&calls, 0, "value\n")}
	if v, err := k.Get(store.SecretController); err != nil || v != "value" {
		t.Errorf("get: %q %v", v, err)
	}
	if got := strings.Join(calls[0].args, " "); got != "find-generic-password -a duet -s duet.controller-secret -w /Library/Keychains/System.keychain" {
		t.Errorf("argv %q", got)
	}
	k.Run = fake(&calls, exitNotFound, "")
	if _, err := k.Get("nope"); !errors.Is(err, store.ErrNoSecret) {
		t.Errorf("missing: %v", err)
	}
	if err := k.Delete("nope"); err != nil {
		t.Errorf("delete missing: %v", err)
	}
	k.Run = fake(&calls, 51, "")
	if _, err := k.Get("x"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("failure: %v", err)
	}
	if err := k.Set("x", "y"); err == nil {
		t.Error("set failure not reported")
	}
}

func TestRefusesBadInput(t *testing.T) {
	var calls []call
	k := &Keychain{Run: fake(&calls, 0, "")}
	for _, key := range []string{"", `a"b`, "a\nb", `a\b`} {
		if err := k.Set(key, "v"); err == nil {
			t.Errorf("key %q accepted", key)
		}
	}
	if len(calls) != 0 {
		t.Errorf("security ran: %v", calls)
	}
}

// TestRealKeychain round-trips through a throwaway keychain file. It does
// not touch the login or System keychain or the search list. Opt in with
// DUET_REAL_KEYCHAIN=1.
func TestRealKeychain(t *testing.T) {
	if os.Getenv("DUET_REAL_KEYCHAIN") != "1" {
		t.Skip("set DUET_REAL_KEYCHAIN=1")
	}
	path := filepath.Join(t.TempDir(), "duet-test.keychain")
	if out, err := exec.Command(securityBin, "create-keychain", "-p", "test", path).CombinedOutput(); err != nil {
		t.Fatalf("create-keychain: %v %s", err, out)
	}
	k := &Keychain{Path: path, Service: "duet-test"}
	if _, err := k.Get("a"); !errors.Is(err, store.ErrNoSecret) {
		t.Fatalf("empty: %v", err)
	}
	v := `p@ss "w" \ $x 'q' 秘密 deadbeef`
	for _, val := range []string{v, "deadbeef", "line\nbreak\x00", "duet-b64:abc", "plain-ascii token"} {
		if err := k.Set("a/b", val); err != nil {
			t.Fatal(err)
		}
		if got, err := k.Get("a/b"); err != nil || got != val {
			t.Fatalf("got %q %v, want %q", got, err, val)
		}
	}
	if err := k.Delete("a/b"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get("a/b"); !errors.Is(err, store.ErrNoSecret) {
		t.Fatalf("after delete: %v", err)
	}
}
