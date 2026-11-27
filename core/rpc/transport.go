package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

// Peer is the identity of the process on the other end of a connection,
// as the platform's PeerOf reads it from the kernel (never from what the
// client says). Logs and audit records name a caller with Caller and
// LogValue, which work on every platform.
type Peer struct {
	UID  int   // -1 on Windows
	GIDs []int // primary group first when the platform reports it
	PID  int   // 0 when unknown
	// Windows only: the user SID of the peer's process token, whether the
	// token is elevated, and whether Administrators is an enabled group of
	// it (only in an elevated admin token; a filtered one has it deny-only).
	SID      string
	Elevated bool
	Admin    bool
	// User is the account name of UID (unix) or SID (Windows,
	// DOMAIN\name); "" when it could not be looked up.
	User string
}

// Transport hides the local socket kind: a unix socket on macOS and Linux,
// a named pipe on Windows (DefaultTransport).
type Transport interface {
	Listen(addr string) (net.Listener, error)
	Dial(ctx context.Context, addr string) (net.Conn, error)
	// PeerOf returns the credentials of the peer of a conn accepted from
	// Listen.
	PeerOf(c net.Conn) (Peer, error)
}

// UnixTransport serves on a unix socket file.
type UnixTransport struct {
	// Mode of the socket file (default 0660).
	Mode os.FileMode
	// GID, if >= 0, is set as the socket file's group (needs root).
	GID int
}

// NewUnixTransport returns a transport with mode 0660 and no chown.
func NewUnixTransport() *UnixTransport { return &UnixTransport{Mode: 0o660, GID: -1} }

// Listen creates the socket. A stale socket file left by a dead helper is
// removed; a live one makes Listen fail. Anything that is not a socket at
// addr is left alone and is an error.
func (t *UnixTransport) Listen(addr string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(addr), 0o755); err != nil {
		return nil, err
	}
	if fi, err := os.Lstat(addr); err == nil {
		if fi.Mode().Type() != os.ModeSocket {
			return nil, fmt.Errorf("%s exists and is not a socket", addr)
		}
		c, err := net.DialTimeout("unix", addr, time.Second)
		if err == nil {
			c.Close()
			return nil, fmt.Errorf("%s: another helper is already listening", addr)
		}
		if err := os.Remove(addr); err != nil {
			return nil, err
		}
	}
	l, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	l.(*net.UnixListener).SetUnlinkOnClose(true)
	mode := t.Mode
	if mode == 0 {
		mode = 0o660
	}
	if t.GID >= 0 {
		if err := os.Chown(addr, -1, t.GID); err != nil {
			l.Close()
			return nil, err
		}
	}
	if err := os.Chmod(addr, mode); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// Dial connects to the socket.
func (t *UnixTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", addr)
}

// PeerOf reads the peer credentials from the kernel (getpeereid /
// LOCAL_PEERCRED on macOS, SO_PEERCRED on Linux).
func (t *UnixTransport) PeerOf(c net.Conn) (Peer, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return Peer{}, errors.New("not a unix socket connection")
	}
	p, err := peerCred(uc)
	if err == nil {
		p.User = userName(p.UID)
	}
	return p, err
}

// Authorizer decides whether a peer may use the helper.
type Authorizer interface {
	Authorize(p Peer) error
}

// GroupAuthorizer allows root, the helper's own uid (a development helper
// running as a normal user), and members of one group (default "admin").
type GroupAuthorizer struct {
	GID     int // -1 disables the group rule
	SelfUID int
	// LookupGroups returns the group ids of a uid; used when the platform
	// does not report the peer's groups. Defaults to os/user.
	LookupGroups func(uid int) ([]int, error)
}

// NewGroupAuthorizer resolves the group name ("" means "admin").
func NewGroupAuthorizer(group string) (*GroupAuthorizer, error) {
	if group == "" {
		group = "admin"
	}
	a := &GroupAuthorizer{GID: -1, SelfUID: os.Getuid()}
	g, err := user.LookupGroup(group)
	if err != nil {
		return nil, fmt.Errorf("group %q: %w", group, err)
	}
	if a.GID, err = strconv.Atoi(g.Gid); err != nil {
		return nil, err
	}
	return a, nil
}

// Authorize implements Authorizer.
func (a *GroupAuthorizer) Authorize(p Peer) error {
	if p.UID == 0 || p.UID == a.SelfUID {
		return nil
	}
	if a.GID < 0 {
		return fmt.Errorf("uid %d is not allowed", p.UID)
	}
	if slices.Contains(p.GIDs, a.GID) {
		return nil
	}
	lookup := a.LookupGroups
	if lookup == nil {
		lookup = osGroups
	}
	gids, err := lookup(p.UID)
	if err == nil && slices.Contains(gids, a.GID) {
		return nil
	}
	return fmt.Errorf("uid %d is not in group %d", p.UID, a.GID)
}

func osGroups(uid int) ([]int, error) {
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return nil, err
	}
	ids, err := u.GroupIds()
	if err != nil {
		return nil, err
	}
	out := make([]int, 0, len(ids))
	for _, s := range ids {
		if n, err := strconv.Atoi(s); err == nil {
			out = append(out, n)
		}
	}
	return out, nil
}

// AllowAll is an Authorizer for tests.
type AllowAll struct{}

func (AllowAll) Authorize(Peer) error { return nil }
