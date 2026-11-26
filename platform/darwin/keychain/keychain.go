//go:build darwin

// Package keychain implements store.Secrets with the macOS keychain through
// the `security` command line tool (no cgo).
//
// Items are generic passwords with account "duet" and service
// "duet.<key>" (e.g. "duet.controller-secret",
// "duet.subscription-url/main"). The helper, running as root, uses the
// System keychain (/Library/Keychains/System.keychain), which is unlocked
// for root without a prompt; development runs can point Path at a test
// keychain.
//
// Keeping secrets off the command line: `security add-generic-password -w
// SECRET` would show SECRET to anyone running ps. Putting -w last makes
// security prompt instead, but it reads from the terminal, and with stdin
// as a pipe (no tty) it stores nothing and still exits 0, so it is not
// usable from a daemon. Instead the whole add command is written to
// `security -i` (interactive mode) on stdin, with the password as hex
// (-X). argv then only shows `security -i`; the secret never appears in
// argv, the environment or a file. Reading uses `find-generic-password -w`,
// whose stdout goes straight into the helper. find -w prints any data that
// is not printable ASCII as hex, which would be ambiguous ("deadbeef"), so
// values that are not printable ASCII (or that start with the marker) are
// stored as "duet-b64:" + base64. Typical secrets (URLs, tokens) stay
// readable in Keychain Access.
package keychain

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/l3ony2k/duet/core/store"
)

// SystemKeychain is the keychain of the root daemon.
const SystemKeychain = "/Library/Keychains/System.keychain"

// Defaults.
const (
	DefaultService = "duet"
	DefaultAccount = "duet"
	securityBin    = "/usr/bin/security"
)

// errSecItemNotFound is security's exit status for a missing item (-25300).
const exitNotFound = 44

// Runner runs security with stdin and returns stdout, stderr and the exit
// status (0 on success; -1 with err set when it could not run).
type Runner func(ctx context.Context, stdin []byte, args ...string) (stdout, stderr []byte, code int, err error)

// Keychain is a store.Secrets in a macOS keychain.
type Keychain struct {
	// Path of the keychain file. Default SystemKeychain.
	Path string
	// Service is the prefix of item service names. Default "duet".
	Service string
	// Account of every item. Default "duet".
	Account string
	// Run defaults to running /usr/bin/security.
	Run Runner
}

var _ store.Secrets = (*Keychain)(nil)

func (k *Keychain) path() string {
	if k.Path != "" {
		return k.Path
	}
	return SystemKeychain
}

func (k *Keychain) service(key string) string {
	s := k.Service
	if s == "" {
		s = DefaultService
	}
	return s + "." + key
}

func (k *Keychain) account() string {
	if k.Account != "" {
		return k.Account
	}
	return DefaultAccount
}

func (k *Keychain) run(ctx context.Context, stdin []byte, args ...string) ([]byte, []byte, int, error) {
	if k.Run != nil {
		return k.Run(ctx, stdin, args...)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, securityBin, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.Bytes(), errb.Bytes(), ee.ExitCode(), nil
	}
	if err != nil {
		return nil, nil, -1, err
	}
	return out.Bytes(), errb.Bytes(), 0, nil
}

// checkName refuses characters that would break the quoting of the
// interactive command line ("security -i" splits on blanks and honours
// double quotes) or that make no sense in a key.
func checkName(what, s string) error {
	if s == "" {
		return fmt.Errorf("keychain: empty %s", what)
	}
	for _, r := range s {
		if r == '"' || r == '\\' || unicode.IsControl(r) {
			return fmt.Errorf("keychain: invalid character %q in %s", r, what)
		}
	}
	return nil
}

// b64Marker marks values stored base64-encoded.
const b64Marker = "duet-b64:"

// encode returns the data stored for v: v itself when it is printable
// ASCII, else the marker and base64.
func encode(v string) string {
	plain := !strings.HasPrefix(v, b64Marker)
	for i := 0; plain && i < len(v); i++ {
		plain = v[i] >= 0x20 && v[i] < 0x7f
	}
	if plain {
		return v
	}
	return b64Marker + base64.StdEncoding.EncodeToString([]byte(v))
}

func decode(s string) (string, error) {
	rest, ok := strings.CutPrefix(s, b64Marker)
	if !ok {
		return s, nil
	}
	b, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		return "", fmt.Errorf("keychain: corrupt encoded value: %w", err)
	}
	return string(b), nil
}

func (k *Keychain) check(key string) error {
	if err := checkName("key", key); err != nil {
		return err
	}
	if err := checkName("keychain path", k.path()); err != nil {
		return err
	}
	return checkName("account", k.account())
}

func failure(op string, stderr []byte, code int) error {
	msg := strings.TrimSpace(string(stderr))
	if msg == "" {
		msg = "no output"
	}
	return fmt.Errorf("keychain %s: security exited %d: %s", op, code, msg)
}

// Get implements store.Secrets.
func (k *Keychain) Get(key string) (string, error) {
	if err := k.check(key); err != nil {
		return "", err
	}
	out, stderr, code, err := k.run(context.Background(), nil,
		"find-generic-password", "-a", k.account(), "-s", k.service(key), "-w", k.path())
	if err != nil {
		return "", fmt.Errorf("keychain get: %w", err)
	}
	switch code {
	case 0:
	case exitNotFound:
		return "", store.ErrNoSecret
	default:
		return "", failure("get", stderr, code)
	}
	return decode(strings.TrimSuffix(string(out), "\n"))
}

// AddCommand is the line written to `security -i` by Set. Exported for
// tests and documentation; it contains the secret (hex-encoded).
func (k *Keychain) AddCommand(key, value string) string {
	return fmt.Sprintf("add-generic-password -U -a \"%s\" -s \"%s\" -l \"%s\" -X %s \"%s\"\n",
		k.account(), k.service(key), k.service(key), hex.EncodeToString([]byte(encode(value))), k.path())
}

// Set implements store.Secrets (adds or updates the item).
func (k *Keychain) Set(key, value string) error {
	if err := k.check(key); err != nil {
		return err
	}
	_, stderr, code, err := k.run(context.Background(), []byte(k.AddCommand(key, value)), "-i")
	if err != nil {
		return fmt.Errorf("keychain set: %w", err)
	}
	if code != 0 {
		return failure("set", stderr, code)
	}
	return nil
}

// Delete implements store.Secrets. A missing item is not an error.
func (k *Keychain) Delete(key string) error {
	if err := k.check(key); err != nil {
		return err
	}
	_, stderr, code, err := k.run(context.Background(), nil,
		"delete-generic-password", "-a", k.account(), "-s", k.service(key), k.path())
	if err != nil {
		return fmt.Errorf("keychain delete: %w", err)
	}
	if code != 0 && code != exitNotFound {
		return failure("delete", stderr, code)
	}
	return nil
}
