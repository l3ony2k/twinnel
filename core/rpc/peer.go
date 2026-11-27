package rpc

import (
	"context"
	"log/slog"
	"os/user"
	"strconv"
	"strings"
	"sync"
)

// Caller names the peer for people and audit records: the account and
// what identifies it on the platform, "leon (uid 501)" or
// `PC\leon (S-1-5-21-…)`; the pid when nothing else is known.
func (p Peer) Caller() string {
	id := ""
	switch {
	case p.SID != "":
		id = p.SID
	case p.UID >= 0:
		id = "uid " + strconv.Itoa(p.UID)
	}
	switch {
	case p.User != "" && id != "":
		return p.User + " (" + id + ")"
	case p.User != "":
		return p.User
	case id != "":
		return id
	case p.PID > 0:
		return "pid " + strconv.Itoa(p.PID)
	}
	return "unknown"
}

// LogValue implements slog.LogValuer: the fields the platform has, so a
// log line reads peer.uid=501 peer.user=leon peer.pid=42 on macOS and
// Linux and peer.sid=S-1-5-21-… peer.user=PC\leon peer.pid=42 on
// Windows (no uid=-1).
func (p Peer) LogValue() slog.Value {
	var as []slog.Attr
	if p.SID != "" {
		as = append(as, slog.String("sid", p.SID))
	} else if p.UID >= 0 {
		as = append(as, slog.Int("uid", p.UID))
	}
	if p.User != "" {
		as = append(as, slog.String("user", p.User))
	}
	if p.PID > 0 {
		as = append(as, slog.Int("pid", p.PID))
	}
	if p.Elevated {
		as = append(as, slog.Bool("elevated", true))
	}
	return slog.GroupValue(as...)
}

type peerKey struct{}

// WithPeer returns ctx carrying the peer of the call (Server sets it for
// every handler).
func WithPeer(ctx context.Context, p Peer) context.Context {
	return context.WithValue(ctx, peerKey{}, p)
}

// PeerFrom is the peer of the call ctx belongs to; ok is false outside a
// call (a test, the helper's own loops).
func PeerFrom(ctx context.Context) (Peer, bool) {
	p, ok := ctx.Value(peerKey{}).(Peer)
	return p, ok
}

// accountNames caches uid or SID → account name: a client connects again
// and again, and the lookup asks the directory service (on Windows maybe
// a domain controller).
var accountNames sync.Map

// cachedName is the cached name for key, looked up the first time.
func cachedName(key any, lookup func() string) string {
	if v, ok := accountNames.Load(key); ok {
		return v.(string)
	}
	name := lookup()
	accountNames.Store(key, name)
	return name
}

// userName is the login name of uid, "" when unknown.
func userName(uid int) string {
	if uid < 0 {
		return ""
	}
	return cachedName(uid, func() string {
		if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
			return strings.TrimSpace(u.Username)
		}
		return ""
	})
}
