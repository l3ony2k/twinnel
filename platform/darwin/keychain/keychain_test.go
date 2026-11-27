//go:build darwin

package keychain

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/l3ony2k/twinnel/core/legacy"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/store"
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
	want := `add-generic-password -U -a "twinnel" -s "twinnel.subscription-url/main" -l "twinnel.subscription-url/main" -X 68747470733a2f2f7375622e6578616d706c652f783f746f6b656e3d733363723374 "/tmp/x.keychain"` + "\n"
	if c.stdin != want {
		t.Errorf("stdin\n got %q\nwant %q", c.stdin, want)
	}
}

func TestEncoding(t *testing.T) {
	for _, v := range []string{"", "abc", "deadbeef", "秘密", "a\nb", b64Marker + "x", "\xff"} {
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
	if got := strings.Join(calls[0].args, " "); got != "find-generic-password -a twinnel -s twinnel.controller-secret -w /Library/Keychains/System.keychain" {
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
// TWINNEL_REAL_KEYCHAIN=1.
func TestRealKeychain(t *testing.T) {
	if os.Getenv("TWINNEL_REAL_KEYCHAIN") != "1" {
		t.Skip("set TWINNEL_REAL_KEYCHAIN=1")
	}
	path := filepath.Join(t.TempDir(), "twinnel-test.keychain")
	if out, err := exec.Command(securityBin, "create-keychain", "-p", "test", path).CombinedOutput(); err != nil {
		t.Fatalf("create-keychain: %v %s", err, out)
	}
	k := &Keychain{Path: path, Service: "twinnel-test"}
	if _, err := k.Get("a"); !errors.Is(err, store.ErrNoSecret) {
		t.Fatalf("empty: %v", err)
	}
	v := `p@ss "w" \ $x 'q' 秘密 deadbeef`
	for _, val := range []string{v, "deadbeef", "line\nbreak\x00", b64Marker + "abc", "plain-ascii token"} {
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

// memKeychain answers find/delete/add (security -i) from a map of
// "account|service" → stored data.
func memKeychain(items map[string]string, calls *[]string) Runner {
	addRE := regexp.MustCompile(`^add-generic-password -U -a "([^"]*)" -s "([^"]*)" -l "[^"]*" -X ([0-9a-f]*) "[^"]*"\n$`)
	return func(_ context.Context, stdin []byte, args ...string) ([]byte, []byte, int, error) {
		*calls = append(*calls, strings.Join(args, " "))
		switch args[0] {
		case "find-generic-password":
			v, ok := items[args[2]+"|"+args[4]]
			if !ok {
				return nil, nil, exitNotFound, nil
			}
			return []byte(v + "\n"), nil, 0, nil
		case "delete-generic-password":
			k := args[2] + "|" + args[4]
			if _, ok := items[k]; !ok {
				return nil, nil, exitNotFound, nil
			}
			delete(items, k)
			return nil, nil, 0, nil
		case "-i":
			m := addRE.FindStringSubmatch(string(stdin))
			if m == nil {
				return nil, []byte("bad add"), 1, nil
			}
			b, _ := hex.DecodeString(m[3])
			items[m[1]+"|"+m[2]] = string(b)
			return nil, nil, 0, nil
		}
		return nil, []byte("unexpected"), 1, nil
	}
}

// An item from before the rename is read, copied to the new name and kept
// (a rolled-back migration needs it); Delete removes both, so a deleted
// secret cannot come back from the old item.
func TestLegacyItems(t *testing.T) {
	items := map[string]string{
		"duet|duet.controller-secret":     "old-secret",
		"duet|duet.subscription-url/main": "https://sub.example/x",
	}
	var calls []string
	k := &Keychain{Run: memKeychain(items, &calls), LegacyService: legacy.KeychainService, LegacyAccount: legacy.KeychainAccount}
	if v, err := k.Get(store.SecretController); err != nil || v != "old-secret" {
		t.Fatalf("get: %q %v", v, err)
	}
	if items["twinnel|twinnel.controller-secret"] != "old-secret" || items["duet|duet.controller-secret"] != "old-secret" {
		t.Errorf("not copied, or the old item removed: %v", items)
	}
	// The new item wins once it exists.
	items["twinnel|twinnel.controller-secret"] = "new-secret"
	if v, _ := k.Get(store.SecretController); v != "new-secret" {
		t.Errorf("get after set: %q", v)
	}
	if err := k.Delete("subscription-url/main"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get("subscription-url/main"); !errors.Is(err, store.ErrNoSecret) {
		t.Errorf("deleted secret came back: %v (%v)", err, items)
	}
	// Without Legacy set, the old items are never looked at.
	items["duet|duet.other"] = "x"
	plain := &Keychain{Run: memKeychain(items, &calls)}
	if _, err := plain.Get("other"); !errors.Is(err, store.ErrNoSecret) {
		t.Errorf("plain keychain read a legacy item: %v", err)
	}
	for _, c := range calls {
		if strings.Contains(c, "secret") && strings.Contains(c, "old-secret") {
			t.Errorf("secret in argv: %s", c)
		}
	}
}
