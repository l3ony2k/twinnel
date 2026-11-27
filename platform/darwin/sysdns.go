//go:build darwin

package darwin

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/l3ony2k/twinnel/core/sysdns"
)

// SystemDNS is sysdns.Platform over networksetup. Run executes a command
// (tests pass a fake); only the helper, as root, may pass a real one for
// -setdnsservers.
type SystemDNS struct {
	Run CommandRunner
}

const networksetup = "/usr/sbin/networksetup"

// Services lists the enabled services with a hardware port (Wi-Fi,
// Ethernet, USB/Thunderbolt adapters): the ones the system-dns check
// looks at.
func (d SystemDNS) Services(ctx context.Context) ([]sysdns.Service, error) {
	out, err := d.Run(ctx, networksetup, "-listnetworkserviceorder")
	if err != nil {
		return nil, err
	}
	var svcs []sysdns.Service
	for _, s := range ParseServiceOrder(string(out)) {
		if !s.Enabled || s.Device == "" {
			continue
		}
		dns, err := d.Run(ctx, networksetup, "-getdnsservers", s.Name)
		if err != nil {
			return nil, err
		}
		svcs = append(svcs, sysdns.Service{Name: s.Name, Servers: ParseGetDNSServers(string(dns))})
	}
	return svcs, nil
}

// Set runs `networksetup -setdnsservers <service> <servers…>` ("Empty"
// for automatic).
func (d SystemDNS) Set(ctx context.Context, name string, servers []string) error {
	args := []string{"-setdnsservers", name}
	if len(servers) == 0 {
		args = append(args, "Empty")
	}
	for _, s := range servers {
		if _, err := netip.ParseAddr(s); err != nil {
			return fmt.Errorf("not an IP address: %q", s)
		}
		args = append(args, s)
	}
	out, err := d.Run(ctx, networksetup, args...)
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
