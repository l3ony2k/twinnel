package main

import (
	"net/netip"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

func TestPeerCells(t *testing.T) {
	p := tailscale.Peer{IPs: []netip.Addr{netip.MustParseAddr("100.64.0.9"), netip.MustParseAddr("fd7a:115c:a1e0::9")}}
	if got := peerIPv6(p).String(); got != "fd7a:115c:a1e0::9" {
		t.Errorf("peerIPv6 = %q", got)
	}
	if got := peerIPs(p); got != "100.64.0.9\nfd7a:115c:a1e0::9" {
		t.Errorf("peerIPs = %q", got)
	}
	if peerIPv6(tailscale.Peer{IPs: []netip.Addr{netip.MustParseAddr("100.64.0.9")}}).IsValid() {
		t.Error("peerIPv6 of a v4-only peer is valid")
	}
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	s := status.Snapshot{Time: now}
	T := i18n.T
	if got := peerSeen(s, tailscale.Peer{Online: true}); got != T.Online {
		t.Errorf("online peer: %q", got)
	}
	if got := peerSeen(s, tailscale.Peer{}); got != T.Offline {
		t.Errorf("offline peer without LastSeen: %q", got)
	}
	ago := tailscale.Peer{LastSeen: now.Add(-48 * time.Hour)}
	if got := peerSeen(s, ago); got != status.Ago(now, ago.LastSeen) {
		t.Errorf("offline peer: %q", got)
	}
}

// The widest IPv4 is what the IP column is sized to: every address fits.
func TestWidestIPv4(t *testing.T) {
	if !netip.MustParseAddr(widestIPv4).Is4() || len(widestIPv4) != len("255.255.255.255") {
		t.Fatal("widestIPv4 is not the widest IPv4")
	}
}
