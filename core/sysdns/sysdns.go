// Package sysdns points the system's DNS at mihomo and puts it back.
//
// A fresh setup makes mihomo the resolver of every active network service
// (dns.listen 127.0.0.1:53): Point records what each service used before
// in a backup file, then sets the target; Restore writes the recorded
// values back and removes the backup. The backup is written once and only
// grows (a service that appears later is added), so pointing twice never
// loses the original values.
//
// The backup never holds the target itself: a service that already
// points at mihomo when it is first recorded (set by hand, or by an
// earlier tool) is recorded as automatic (DHCP), the only value that
// still resolves once mihomo is gone. Adopt records that for services
// that point at mihomo without a backup entry, without changing any DNS
// setting, so the DNS watchdog (core/dnswatch) can protect them.
//
// PointsAt is the one definition of "this service resolves through
// mihomo" for the helper, the watchdog and the system-dns health check.
// LinkConnected and Counted are the one definition of "which services
// count": every verdict about the system DNS (dns.status pointed and
// unprotected, the setup's dns step, the system-dns check) looks only at
// connected services, so a disconnected adapter (a Bluetooth PAN, an
// unplugged Ethernet port) never holds a working machine at "not
// pointed". Actions are wider on purpose: dns.point, dns.restore, Adopt
// and the watchdog act on every managed service, connected or not, so a
// network that connects later already resolves through mihomo, and is
// put back with the others when mihomo stops.
//
// The platform part (networksetup on macOS, netsh and the registry on
// Windows) is behind Platform; tests use a fake.
package sysdns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/l3ony2k/twinnel/core/privfile"
)

// Service is one network service and its DNS servers; no servers means
// the servers come from DHCP / the network ("automatic").
type Service struct {
	Name    string   `json:"name"`
	Servers []string `json:"servers"`
	// Connected: the service's link carries traffic now (LinkConnected).
	// Platform.Services sets it; the backup never stores it.
	Connected bool `json:"connected,omitempty"`
}

// LinkConnected is the one rule for "this network link carries traffic
// now": it is up, has carrier, and holds a routable IPv4 address (not
// link-local: a link with only 169.254.x.x got no lease). mihomo's DNS
// listener and TUN are IPv4, so an IPv4 address is what makes a link's
// DNS matter. Platforms pass what they know: Windows has one operational
// state for up and carrier.
func LinkConnected(up, carrier bool, addrs []netip.Addr) bool {
	if !up || !carrier {
		return false
	}
	for _, a := range addrs {
		if a.Is4() && !a.IsLinkLocalUnicast() && !a.IsLoopback() && !a.IsUnspecified() {
			return true
		}
	}
	return false
}

// Counted returns the services a verdict about the system DNS looks at:
// the connected ones. While none is connected (offline, or a platform
// that cannot tell) every service counts, so the verdict still says
// whether the settings point at mihomo.
func Counted(svcs []Service) []Service {
	var out []Service
	for _, s := range svcs {
		if s.Connected {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return svcs
	}
	return out
}

// Platform reads and sets the DNS of network services.
type Platform interface {
	// Services lists the services DNS is managed on (macOS: enabled
	// services with a hardware port), with their manual servers.
	Services(ctx context.Context) ([]Service, error)
	// Set sets the manual servers of a service; none means automatic.
	Set(ctx context.Context, name string, servers []string) error
}

// DefaultTarget is where dns.point sends the system's queries: mihomo's
// DNS listener of the built-in base settings (dns.listen 127.0.0.1:53).
// A Platform with a DNSTarget method sets its own (Windows adds ::1).
var DefaultTarget = []string{"127.0.0.1"}

// TargetOf is p's target, else DefaultTarget. Its first entry is mihomo's
// listener (what dns.point probes).
func TargetOf(p Platform) []string {
	if t, ok := p.(interface{ DNSTarget() []string }); ok {
		if v := t.DNSTarget(); len(v) > 0 {
			return slices.Clone(v)
		}
	}
	return slices.Clone(DefaultTarget)
}

// PointsAt reports whether a service with these DNS servers resolves
// through mihomo: it has at least one server and every one is among
// mihomo's addresses (MihomoAddrs, or what dns.point sets). A subset
// counts: Windows' IPv4-only 127.0.0.1, set by hand without the ::1 that
// dns.point adds, still sends every query to mihomo.
func PointsAt(servers, mihomo []string) bool {
	if len(servers) == 0 || len(mihomo) == 0 {
		return false
	}
	for _, s := range servers {
		if !slices.Contains(mihomo, s) {
			return false
		}
	}
	return true
}

// MihomoAddrs are the resolver addresses that reach mihomo's DNS listener
// whose host is listenHost (dns.listen without the port), on goos: the
// listener's own address, the IPv4 loopback when it listens on every
// address, and on Windows ::1 (dns.point sets it as the IPv6 server: no
// one answers there, so IPv6 queries fail on the host instead of leaving
// it; platform/windows/sysdns.go).
func MihomoAddrs(listenHost, goos string) []string {
	var out []string
	add := func(a string) {
		if a != "" && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	switch listenHost {
	case "", "0.0.0.0", "::":
		add("127.0.0.1")
	default:
		add(listenHost)
	}
	if goos == "windows" && slices.Contains(out, "127.0.0.1") {
		add("::1")
	}
	return out
}

// Backup is what the services used before Point.
type Backup struct {
	At       time.Time `json:"at"`
	Services []Service `json:"services"`
}

// restoreValue is what restoring s writes: its recorded servers, or
// automatic when they point at mihomo (a backup an older Twinnel wrote
// while the service already used mihomo: writing that back would keep
// the service on a resolver that is gone).
func (s Service) restoreValue(target []string) []string {
	if PointsAt(s.Servers, target) {
		return nil
	}
	return s.Servers
}

// RestoreValue is what restoring the service named name writes back, and
// whether the backup has it.
func (b *Backup) RestoreValue(name string, target []string) ([]string, bool) {
	if b == nil {
		return nil, false
	}
	i := slices.IndexFunc(b.Services, func(s Service) bool { return s.Name == name })
	if i < 0 {
		return nil, false
	}
	return slices.Clone(b.Services[i].restoreValue(target)), true
}

// Status is the current state.
type Status struct {
	Services []Service `json:"services"`
	Target   []string  `json:"target,omitempty"`
	// Pointed: every counted service (Counted: the connected ones)
	// points at mihomo (PointsAt Target).
	Pointed bool    `json:"pointed"`
	Backup  *Backup `json:"backup,omitempty"`
	// Unprotected are the counted services that point at mihomo with no
	// restore value in the backup: if mihomo stops they lose DNS, and
	// nothing can put them back (Adopt records one).
	Unprotected []string `json:"unprotected,omitempty"`
	// Changed lists the services Point or Restore set.
	Changed []string `json:"changed,omitempty"`
	// Recorded lists the services Point or Adopt added to the backup as
	// automatic because they already pointed at mihomo.
	Recorded []string `json:"recorded,omitempty"`
}

// Manager points and restores, keeping the backup at Path.
type Manager struct {
	P    Platform
	Path string
	// Target is what Point sets and what counts as mihomo (PointsAt).
	Target []string
	Now    func() time.Time
}

// ErrNoBackup: Restore found nothing to restore.
var ErrNoBackup = errors.New("no DNS backup: the system DNS was not changed by Twinnel")

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// ReadBackup returns the backup, nil when there is none.
func (m *Manager) ReadBackup() (*Backup, error) {
	b, err := os.ReadFile(m.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var bk Backup
	if err := json.Unmarshal(b, &bk); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(m.Path), err)
	}
	return &bk, nil
}

func (m *Manager) writeBackup(bk *Backup) error {
	b, err := json.MarshalIndent(bk, "", "  ")
	if err != nil {
		return err
	}
	if err := privfile.MkdirAll(filepath.Dir(m.Path)); err != nil {
		return err
	}
	return privfile.WriteFile(m.Path, append(b, '\n'))
}

// Status reads the services and the backup.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	svcs, err := m.P.Services(ctx)
	if err != nil {
		return Status{}, err
	}
	bk, err := m.ReadBackup()
	if err != nil {
		return Status{}, err
	}
	return m.status(svcs, bk), nil
}

func (m *Manager) status(svcs []Service, bk *Backup) Status {
	st := Status{Services: svcs, Target: m.Target, Backup: bk}
	counted := Counted(svcs)
	st.Pointed = len(m.Target) > 0 && len(counted) > 0
	for _, s := range counted {
		if !PointsAt(s.Servers, m.Target) {
			st.Pointed = false
			continue
		}
		if _, ok := bk.RestoreValue(s.Name, m.Target); !ok {
			st.Unprotected = append(st.Unprotected, s.Name)
		}
	}
	return st
}

// record adds the services bk lacks, each with its current servers, or
// automatic when it already points at mihomo; and turns entries that
// point at mihomo into automatic (see Service.restoreValue). It returns
// whether bk changed and the services recorded as automatic.
func (m *Manager) record(bk *Backup, svcs []Service, only func(Service) bool) (bool, []string) {
	changed := false
	var auto []string
	for i, b := range bk.Services {
		if b.Servers != nil && PointsAt(b.Servers, m.Target) {
			bk.Services[i].Servers = nil
			changed = true
		}
	}
	for _, s := range svcs {
		if slices.ContainsFunc(bk.Services, func(b Service) bool { return b.Name == s.Name }) || !only(s) {
			continue
		}
		rec := Service{Name: s.Name, Servers: slices.Clone(s.Servers)} // never Connected: a backup holds settings, not state
		if PointsAt(s.Servers, m.Target) {
			rec.Servers = nil
			auto = append(auto, s.Name)
		}
		bk.Services = append(bk.Services, rec)
		changed = true
	}
	return changed, auto
}

func (m *Manager) needTarget() error {
	if len(m.Target) == 0 {
		return errors.New("no DNS servers to point at")
	}
	return nil
}

// Point sets every service to Target, connected or not (see the package
// comment), recording the earlier values first.
func (m *Manager) Point(ctx context.Context) (Status, error) {
	if err := m.needTarget(); err != nil {
		return Status{}, err
	}
	svcs, err := m.P.Services(ctx)
	if err != nil {
		return Status{}, err
	}
	if len(svcs) == 0 {
		return Status{}, errors.New("no active network service")
	}
	bk, err := m.ReadBackup()
	if err != nil {
		return Status{}, err
	}
	if bk == nil {
		bk = &Backup{At: m.now()}
	}
	grew, auto := m.record(bk, svcs, func(Service) bool { return true })
	if grew {
		// Written before anything changes: a failure half way can always
		// be restored.
		if err := m.writeBackup(bk); err != nil {
			return Status{}, err
		}
	}
	var changed []string
	for _, s := range svcs {
		if slices.Equal(s.Servers, m.Target) {
			continue
		}
		if err := m.P.Set(ctx, s.Name, m.Target); err != nil {
			return Status{Changed: changed, Recorded: auto}, fmt.Errorf("set DNS of %s: %w", s.Name, err)
		}
		changed = append(changed, s.Name)
	}
	st, err := m.Status(ctx)
	st.Changed, st.Recorded = changed, auto
	return st, err
}

// Adopt records automatic (DHCP) as the restore value of every
// unprotected service (Status.Unprotected: a counted service that points
// at mihomo without one), so Restore and the DNS watchdog can put it
// back. It changes no DNS setting and works while mihomo is down.
func (m *Manager) Adopt(ctx context.Context) (Status, error) {
	if err := m.needTarget(); err != nil {
		return Status{}, err
	}
	svcs, err := m.P.Services(ctx)
	if err != nil {
		return Status{}, err
	}
	bk, err := m.ReadBackup()
	if err != nil {
		return Status{}, err
	}
	if bk == nil {
		bk = &Backup{At: m.now()}
	}
	// Exactly the Unprotected ones (counted services): what dns.status
	// and the system-dns check name, and what the fix's plan lists. A
	// disconnected service is recorded once it connects (dns.point
	// records every service).
	unprotected := m.status(svcs, bk).Unprotected
	pick := func(s Service) bool { return slices.Contains(unprotected, s.Name) }
	if changed, auto := m.record(bk, svcs, pick); changed {
		if err := m.writeBackup(bk); err != nil {
			return Status{}, err
		}
		st := m.status(svcs, bk)
		st.Recorded = auto
		return st, nil
	}
	return m.status(svcs, bk), nil
}

// Restore writes the backup's values back (to the services that still
// exist) and removes the backup.
func (m *Manager) Restore(ctx context.Context) (Status, error) {
	bk, err := m.ReadBackup()
	if err != nil {
		return Status{}, err
	}
	if bk == nil {
		return Status{}, ErrNoBackup
	}
	svcs, err := m.P.Services(ctx)
	if err != nil {
		return Status{}, err
	}
	var changed []string
	for _, b := range bk.Services {
		want := b.restoreValue(m.Target)
		i := slices.IndexFunc(svcs, func(s Service) bool { return s.Name == b.Name })
		if i < 0 || slices.Equal(svcs[i].Servers, want) {
			continue // gone, or already as before
		}
		if err := m.P.Set(ctx, b.Name, want); err != nil {
			return Status{Changed: changed}, fmt.Errorf("restore DNS of %s: %w", b.Name, err)
		}
		changed = append(changed, b.Name)
	}
	if err := os.Remove(m.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Status{Changed: changed}, err
	}
	st, err := m.Status(ctx)
	st.Changed = changed
	return st, err
}
