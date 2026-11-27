//go:build darwin

package darwin

import (
	"bufio"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/l3ony2k/twinnel/core/health"
)

func lines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

// keyVal splits "  key : value" lines.
func keyVal(l string) (string, string, bool) {
	k, v, ok := strings.Cut(l, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(k), strings.TrimSpace(v), true
}

// ParseRouteGet parses `route -n get <dest>`.
func ParseRouteGet(out string) health.RouteLookup {
	var r health.RouteLookup
	for _, l := range lines(out) {
		k, v, ok := keyVal(l)
		if !ok {
			continue
		}
		switch k {
		case "destination":
			r.Destination = v
		case "gateway":
			r.Gateway = v
		case "interface":
			r.Interface = v
		case "flags":
			v = strings.Trim(v, "<>")
			if v != "" {
				r.Flags = strings.Split(v, ",")
			}
		}
	}
	return r
}

// ParseNetstat parses `netstat -rn`.
func ParseNetstat(out string) []health.Route {
	var routes []health.Route
	family := 0
	for _, l := range lines(out) {
		switch strings.TrimSpace(l) {
		case "Internet:":
			family = 4
			continue
		case "Internet6:":
			family = 6
			continue
		}
		f := strings.Fields(l)
		if family == 0 || len(f) < 4 || f[0] == "Destination" {
			continue
		}
		routes = append(routes, health.Route{Family: family, Destination: f[0], Gateway: f[1], Flags: f[2], Interface: f[3]})
	}
	return routes
}

// ParseIfconfig parses `ifconfig` (all interfaces).
func ParseIfconfig(out string) []health.Interface {
	var ifs []health.Interface
	var cur *health.Interface
	hasStatus := false
	p2p := false
	flush := func() {
		if cur == nil {
			return
		}
		if !hasStatus {
			cur.Active = cur.Up && p2p
		}
		ifs = append(ifs, *cur)
	}
	for _, l := range lines(out) {
		if l == "" {
			continue
		}
		if l[0] != '\t' && l[0] != ' ' {
			flush()
			name, rest, _ := strings.Cut(l, ": ")
			cur = &health.Interface{Name: name}
			hasStatus = false
			flags := ""
			if i, j := strings.Index(rest, "<"), strings.Index(rest, ">"); i >= 0 && j > i {
				flags = rest[i+1 : j]
			}
			fl := "," + flags + ","
			cur.Up = strings.Contains(fl, ",UP,") && strings.Contains(fl, ",RUNNING,")
			p2p = strings.Contains(fl, ",POINTOPOINT,")
			continue
		}
		if cur == nil {
			continue
		}
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "inet", "inet6":
			a := f[1]
			if i := strings.IndexByte(a, '%'); i >= 0 {
				a = a[:i]
			}
			if ip, err := netip.ParseAddr(a); err == nil {
				cur.Addrs = append(cur.Addrs, ip)
			}
		case "status:":
			hasStatus = true
			cur.Active = f[1] == "active"
		}
	}
	flush()
	return ifs
}

// ParseScutilDNS parses `scutil --dns`.
func ParseScutilDNS(out string) []health.Resolver {
	var rs []health.Resolver
	var cur *health.Resolver
	scoped := false
	flush := func() {
		if cur != nil {
			rs = append(rs, *cur)
			cur = nil
		}
	}
	for _, l := range lines(out) {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "DNS configuration"):
			flush()
			scoped = strings.Contains(t, "scoped")
			continue
		case strings.HasPrefix(t, "resolver #"):
			flush()
			cur = &health.Resolver{Scoped: scoped}
			continue
		}
		if cur == nil {
			continue
		}
		k, v, ok := keyVal(t)
		if !ok {
			continue
		}
		switch {
		case k == "domain":
			cur.Domain = v
		case strings.HasPrefix(k, "nameserver["):
			cur.Nameservers = append(cur.Nameservers, v)
		case strings.HasPrefix(k, "search domain["):
			cur.SearchDomains = append(cur.SearchDomains, v)
		case k == "options":
			cur.Options = v
		case k == "if_index":
			cur.IfIndex = v
		case k == "flags":
			if strings.Contains(v, "Scoped") {
				cur.Scoped = true
			}
		}
	}
	flush()
	return rs
}

var ncLine = regexp.MustCompile(`^(\*)?\s*\(([^)]*)\)\s+([0-9A-Fa-f-]{36})\s+(.*?)\s+"([^"]*)"\s*(?:\[([^\]]*)\])?\s*$`)

// ParseNCList parses `scutil --nc list`.
func ParseNCList(out string) []health.VPN {
	var vpns []health.VPN
	for _, l := range lines(out) {
		m := ncLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		v := health.VPN{Enabled: m[1] == "*", Status: m[2], Kind: strings.TrimSpace(m[4]), Name: m[5]}
		if _, b, ok := strings.Cut(m[6], ":"); ok {
			v.BundleID = b
		}
		vpns = append(vpns, v)
	}
	return vpns
}

var (
	orderHead = regexp.MustCompile(`^\((\d+|\*)\)\s+(.+)$`)
	orderPort = regexp.MustCompile(`^\(Hardware Port: (.*), Device: (.*)\)$`)
)

// ParseServiceOrder parses `networksetup -listnetworkserviceorder`.
// DNS servers are filled in separately.
func ParseServiceOrder(out string) []health.NetworkService {
	var svcs []health.NetworkService
	for _, l := range lines(out) {
		l = strings.TrimSpace(l)
		if m := orderHead.FindStringSubmatch(l); m != nil {
			s := health.NetworkService{Name: m[2], Enabled: m[1] != "*"}
			if n, err := strconv.Atoi(m[1]); err == nil {
				s.Order = n
			}
			svcs = append(svcs, s)
			continue
		}
		if m := orderPort.FindStringSubmatch(l); m != nil && len(svcs) > 0 {
			svcs[len(svcs)-1].Device = strings.TrimSpace(m[2])
		}
	}
	return svcs
}

// ParseGetDNSServers parses `networksetup -getdnsservers <service>`.
// It returns nil when the service uses DHCP-provided DNS.
func ParseGetDNSServers(out string) []string {
	var ns []string
	for _, l := range lines(out) {
		l = strings.TrimSpace(l)
		if _, err := netip.ParseAddr(l); err == nil {
			ns = append(ns, l)
		}
	}
	return ns
}

// ParseDscacheutil parses `dscacheutil -q host -a name <name>`.
func ParseDscacheutil(name, out string) health.DNSAnswer {
	ans := health.DNSAnswer{Name: name}
	for _, l := range lines(out) {
		k, v, ok := keyVal(l)
		if !ok || (k != "ip_address" && k != "ipv6_address") {
			continue
		}
		if a, err := netip.ParseAddr(v); err == nil {
			ans.Addrs = append(ans.Addrs, a)
		}
	}
	if len(ans.Addrs) == 0 {
		ans.Rcode = "NXDOMAIN"
	} else {
		ans.Rcode = "NOERROR"
	}
	return ans
}
