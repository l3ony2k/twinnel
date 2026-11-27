//go:build windows

package windows

import (
	"context"
	"errors"
	"net/netip"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	xwin "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/l3ony2k/twinnel/core/health"
)

// NewSys returns the real Sys: GetAdaptersAddresses, GetIpForwardTable2 and
// registry reads. All read-only.
func NewSys() Sys { return realSys{} }

type realSys struct{}

func (realSys) Adapters(context.Context) ([]Adapter, error) {
	const flags = xwin.GAA_FLAG_INCLUDE_GATEWAYS | xwin.GAA_FLAG_SKIP_ANYCAST | xwin.GAA_FLAG_SKIP_MULTICAST
	size := uint32(16 << 10)
	var buf []byte
	for range 4 {
		buf = make([]byte, size)
		err := xwin.GetAdaptersAddresses(xwin.AF_UNSPEC, flags, 0, (*xwin.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if !errors.Is(err, xwin.ERROR_BUFFER_OVERFLOW) {
			return nil, err
		}
		buf = nil
	}
	if buf == nil {
		return nil, errors.New("GetAdaptersAddresses: buffer keeps growing")
	}
	var out []Adapter
	for p := (*xwin.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); p != nil; p = p.Next {
		a := Adapter{
			Index:       p.IfIndex,
			GUID:        xwin.BytePtrToString(p.AdapterName),
			Name:        xwin.UTF16PtrToString(p.FriendlyName),
			Description: xwin.UTF16PtrToString(p.Description),
			IfType:      p.IfType,
			Up:          p.OperStatus == xwin.IfOperStatusUp,
			DNSSuffix:   xwin.UTF16PtrToString(p.DnsSuffix),
			Metric4:     p.Ipv4Metric,
			Metric6:     p.Ipv6Metric,
		}
		if a.Index == 0 {
			a.Index = p.Ipv6IfIndex
		}
		for u := p.FirstUnicastAddress; u != nil; u = u.Next {
			if ip, ok := sockAddr(&u.Address); ok {
				a.Addrs = append(a.Addrs, ip)
			}
		}
		for d := p.FirstDnsServerAddress; d != nil; d = d.Next {
			if ip, ok := sockAddr(&d.Address); ok {
				a.DNS = append(a.DNS, ip)
			}
		}
		for g := p.FirstGatewayAddress; g != nil; g = g.Next {
			if ip, ok := sockAddr(&g.Address); ok {
				a.Gateways = append(a.Gateways, ip)
			}
		}
		out = append(out, a)
	}
	return out, nil
}

func sockAddr(s *xwin.SocketAddress) (netip.Addr, bool) {
	if s == nil || s.Sockaddr == nil {
		return netip.Addr{}, false
	}
	ip := s.IP()
	if ip == nil {
		return netip.Addr{}, false
	}
	a, ok := netip.AddrFromSlice(ip)
	return a.Unmap(), ok
}

func rawInet(r *xwin.RawSockaddrInet) netip.Addr {
	switch r.Family {
	case xwin.AF_INET:
		return netip.AddrFrom4((*xwin.RawSockaddrInet4)(unsafe.Pointer(r)).Addr)
	case xwin.AF_INET6:
		return netip.AddrFrom16((*xwin.RawSockaddrInet6)(unsafe.Pointer(r)).Addr)
	}
	return netip.Addr{}
}

func (realSys) Routes(context.Context) ([]RouteRow, error) {
	var t *xwin.MibIpForwardTable2
	if err := xwin.GetIpForwardTable2(xwin.AF_UNSPEC, &t); err != nil {
		return nil, err
	}
	defer xwin.FreeMibTable(unsafe.Pointer(t))
	var out []RouteRow
	for _, r := range t.Rows() {
		dst := rawInet(&r.DestinationPrefix.Prefix)
		if !dst.IsValid() {
			continue
		}
		p, err := dst.Prefix(int(r.DestinationPrefix.PrefixLength))
		if err != nil {
			continue
		}
		out = append(out, RouteRow{Prefix: p, NextHop: rawInet(&r.NextHop), IfIndex: r.InterfaceIndex, Metric: r.Metric})
	}
	return out, nil
}

// nrptKeys hold NRPT rules: the local ones (what Tailscale writes) and the
// group policy ones.
var nrptKeys = []string{
	`SYSTEM\CurrentControlSet\Services\Dnscache\Parameters\DnsPolicyConfig`,
	`SOFTWARE\Policies\Microsoft\Windows NT\DNSClient\DnsPolicyConfig`,
}

func (realSys) NRPT(context.Context) ([]NRPTRule, error) {
	var out []NRPTRule
	for _, path := range nrptKeys {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
		if err != nil {
			continue // no rules
		}
		names, _ := k.ReadSubKeyNames(-1)
		for _, n := range names {
			sk, err := registry.OpenKey(k, n, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			var r NRPTRule
			if ns, _, err := sk.GetStringsValue("Name"); err == nil {
				r.Namespaces = ns
			} else if s, _, err := sk.GetStringValue("Name"); err == nil {
				r.Namespaces = []string{s}
			}
			if s, _, err := sk.GetStringValue("GenericDNSServers"); err == nil {
				for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' || r == ' ' }) {
					r.Servers = append(r.Servers, f)
				}
			}
			sk.Close()
			if len(r.Namespaces) > 0 && len(r.Servers) > 0 {
				out = append(out, r)
			}
		}
		k.Close()
	}
	return out, nil
}

func (realSys) DNSSource(guid string) string {
	if guid == "" {
		return ""
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces\`+guid, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	if s, _, err := k.GetStringValue("NameServer"); err == nil && strings.TrimSpace(s) != "" {
		return health.DNSSourceManual
	}
	return health.DNSSourceAuto
}

// hideWindow keeps a console program started from the GUI app from
// opening a console window.
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= xwin.CREATE_NO_WINDOW
}
