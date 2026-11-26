package kernel

// Running external binaries from a root helper.
//
// The helper runs a few programs it does not own: the tailscale CLI (a
// Homebrew install is owned by the user who ran brew), an adopted mihomo
// binary (often in the user's home), the managed kernel. Exec'ing a binary
// that a non-root user can replace, as root, hands that user root: they
// swap the file and wait for the next poll. So the helper first looks at
// who controls the binary (the file and every directory above it) and, if
// that is not root alone, runs it with the privileges of whoever does
// control it (the owner), or as nobody when several users or a
// world-writable directory are involved. That user could already run the
// binary themselves, so nothing is gained by replacing it.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Credential is a uid/gid pair a command runs as.
type Credential struct {
	UID, GID int
	// Home is the user's home dir ("" for nobody); HOME is set to it.
	Home string
	Name string
}

// Nobody is the unprivileged account used when no single owner controls a
// binary (macOS and the BSDs: uid/gid -2).
var Nobody = Credential{UID: -2 & 0xffffffff, GID: -2 & 0xffffffff, Name: "nobody", Home: "/var/empty"}

// ExecTrust says who controls a binary.
type ExecTrust struct {
	// Path is the binary after resolving symlinks; run this path, not the
	// one given, so a link swapped later is not followed.
	Path string
	// Trusted: the file and every directory above it are owned by root and
	// not writable by group or others (sticky directories excepted).
	Trusted bool
	// RunAs is the user to run as when not Trusted.
	RunAs Credential
	// Why explains an untrusted result, for logs and warnings.
	Why string
}

// trustHooks are replaceable in tests (they cannot create root-owned files).
var (
	trustGeteuid = os.Geteuid
	trustLstat   = os.Lstat
	trustLookup  = lookupCredential
)

// CheckExec resolves bin and reports who controls it.
func CheckExec(bin string) (ExecTrust, error) {
	if !strings.Contains(bin, string(filepath.Separator)) {
		p, err := exec.LookPath(bin)
		if err != nil {
			return ExecTrust{}, err
		}
		bin = p
	}
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return ExecTrust{}, err
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return ExecTrust{}, err
	}
	t := ExecTrust{Path: real, Trusted: true}
	owners := map[int]string{}
	worldWritable, groupWritable := "", ""
	for p := real; ; p = filepath.Dir(p) {
		fi, err := trustLstat(p)
		if err != nil {
			return ExecTrust{}, err
		}
		uid, gid, ok := fileOwner(fi)
		if !ok {
			return ExecTrust{}, fmt.Errorf("%s: no owner information", p)
		}
		if uid != 0 {
			if _, seen := owners[uid]; !seen {
				owners[uid] = p
			}
		}
		mode := fi.Mode()
		sticky := mode&fs.ModeSticky != 0 && p != real
		switch {
		case mode.Perm()&0o002 != 0 && !sticky:
			worldWritable = p
		case mode.Perm()&0o020 != 0 && !sticky && gid != 0:
			// Group-writable by a non-wheel group (Homebrew's prefix is
			// group admin): any member can replace it.
			groupWritable = p
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	switch {
	case worldWritable != "":
		t.Trusted, t.RunAs = false, Nobody
		t.Why = worldWritable + " is writable by group or others"
	case len(owners) > 1:
		t.Trusted, t.RunAs = false, Nobody
		t.Why = "several users own parts of the path"
	case len(owners) == 0 && groupWritable != "":
		t.Trusted, t.RunAs = false, Nobody
		t.Why = groupWritable + " is writable by its group"
	case len(owners) == 1:
		// Run as the owner, also when a directory is group-writable: a
		// group member could reach that user's privileges, never root's.
		for uid, p := range owners {
			c, err := trustLookup(uid)
			if err != nil {
				c = Nobody
			}
			t.Trusted, t.RunAs = false, c
			t.Why = fmt.Sprintf("%s is owned by uid %d", p, uid)
		}
	}
	return t, nil
}

// Command returns an exec.Cmd for bin that never runs a binary a non-root
// user controls with root privileges: when the helper is root and bin is
// not Trusted, the command runs as ExecTrust.RunAs. As a normal user (tests,
// development) it is plain exec.CommandContext.
func Command(ctx context.Context, bin string, args ...string) (*exec.Cmd, error) {
	if trustGeteuid() != 0 {
		return exec.CommandContext(ctx, bin, args...), nil
	}
	t, err := CheckExec(bin)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, t.Path, args...)
	if !t.Trusted {
		dropTo(cmd, t.RunAs)
	}
	return cmd, nil
}

// Output runs Command and returns stdout (like exec.Cmd.Output).
func Output(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd, err := Command(ctx, bin, args...)
	if err != nil {
		return nil, err
	}
	return cmd.Output()
}

// CombinedOutput runs Command and returns stdout and stderr.
func CombinedOutput(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd, err := Command(ctx, bin, args...)
	if err != nil {
		return nil, err
	}
	return cmd.CombinedOutput()
}

// RunAsFor returns the credential bin must run under when the helper is
// root: nil when bin is Trusted (or the helper is not root).
func RunAsFor(bin string) (*Credential, error) {
	if trustGeteuid() != 0 {
		return nil, nil
	}
	t, err := CheckExec(bin)
	if err != nil {
		return nil, err
	}
	if t.Trusted {
		return nil, nil
	}
	c := t.RunAs
	return &c, nil
}

// ErrUntrusted is wrapped by RequireTrusted.
var ErrUntrusted = errors.New("binary is writable by a non-root user")

// RequireTrusted fails when bin is not Trusted: for binaries that end up in
// a root LaunchDaemon, where dropping privileges is not possible.
func RequireTrusted(bin string) error {
	t, err := CheckExec(bin)
	if err != nil {
		return err
	}
	if !t.Trusted {
		return fmt.Errorf("%s: %w (%s)", bin, ErrUntrusted, t.Why)
	}
	return nil
}
