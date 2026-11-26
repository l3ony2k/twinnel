package config

import (
	"fmt"
	"math/rand"
	"net/netip"
	"strings"
	"testing"
)

// simConn is a connection for the rule simulator.
type simConn struct {
	host    string     // "" for a bare IP connection
	ip      netip.Addr // destination IP (for a host: what DNS would answer)
	hasIP   bool       // a host connection already carries its IP (sniffed / redir-host)
	udp     bool
	process string
}

// simRoute is a small model of mihomo's tunnel.match: first matching rule
// wins, except that a target without UDP support is skipped for UDP
// connections (here: "NOUDP"); no match means DIRECT.
func simRoute(rules []string, c simConn) string {
	for _, l := range rules {
		r := ParseRule(l)
		if !simMatch(r, c) {
			continue
		}
		if c.udp && r.Target == "NOUDP" {
			continue
		}
		return r.Target
	}
	return "DIRECT"
}

func simMatch(r Rule, c simConn) bool {
	host := strings.ToLower(c.host)
	switch r.Type {
	case "MATCH":
		return true
	case "DOMAIN":
		return host != "" && host == r.Payload
	case "DOMAIN-SUFFIX":
		return host != "" && (host == r.Payload || strings.HasSuffix(host, "."+r.Payload))
	case "DOMAIN-KEYWORD":
		return host != "" && strings.Contains(host, r.Payload)
	case "IP-CIDR":
		p, err := netip.ParsePrefix(r.Payload)
		if err != nil {
			return false
		}
		if c.host != "" && !c.hasIP && r.noResolve() {
			return false
		}
		return p.Contains(c.ip)
	case "GEOIP":
		// Opaque database: "CN" means 10.0.0.0/8 here.
		if c.host != "" && !c.hasIP && r.noResolve() {
			return false
		}
		return r.Payload == "CN" && netip.MustParsePrefix("10.0.0.0/8").Contains(c.ip)
	case "PROCESS-NAME":
		return c.process == r.Payload
	case "AND":
		// AND,((NETWORK,udp),(DOMAIN-SUFFIX,x))
		return c.udp && simMatch(ParseRule("DOMAIN-SUFFIX,"+strings.TrimSuffix(strings.TrimPrefix(r.Payload, "((NETWORK,udp),(DOMAIN-SUFFIX,"), "))")+",X"), c)
	}
	return false
}

var simHosts = []string{"a.com", "x.a.com", "y.x.a.com", "b.com", "ab.com", "cn.net", "x.cn", "cnn.com", "Big.A.com"}

func simRule(r *rand.Rand) string {
	targets := []string{"DIRECT", "REJECT", "PROXY", "NOUDP", "Proxy"}
	t := targets[r.Intn(len(targets))]
	doms := []string{"a.com", "x.a.com", "b.com", "cn", ".cn", ".a.com", "com", "A.com"}
	d := doms[r.Intn(len(doms))]
	switch r.Intn(9) {
	case 0:
		return "DOMAIN," + d + "," + t
	case 1, 2:
		return "DOMAIN-SUFFIX," + d + "," + t
	case 3:
		return "DOMAIN-KEYWORD," + []string{"a", "cn", ".cn", "x.a", "b"}[r.Intn(5)] + "," + t
	case 4:
		p := []string{"10.0.0.0/8", "10.1.0.0/16", "10.1.2.0/24", "192.168.0.0/16", "0.0.0.0/0"}[r.Intn(5)]
		if r.Intn(2) == 0 {
			return "IP-CIDR," + p + "," + t + ",no-resolve"
		}
		return "IP-CIDR," + p + "," + t
	case 5:
		if r.Intn(2) == 0 {
			return "GEOIP,CN," + t + ",no-resolve"
		}
		return "GEOIP,CN," + t
	case 6:
		return "PROCESS-NAME,curl," + t
	case 7:
		return "AND,((NETWORK,udp),(DOMAIN-SUFFIX," + d + "))," + t
	}
	return "MATCH," + t
}

func simConns() []simConn {
	ips := []string{"10.1.2.3", "10.9.0.1", "192.168.1.1", "8.8.8.8"}
	var out []simConn
	for _, udp := range []bool{false, true} {
		for _, proc := range []string{"", "curl"} {
			for _, ip := range ips {
				out = append(out, simConn{ip: netip.MustParseAddr(ip), udp: udp, process: proc})
				for _, h := range simHosts {
					out = append(out, simConn{host: h, ip: netip.MustParseAddr(ip), udp: udp, process: proc})
					out = append(out, simConn{host: h, ip: netip.MustParseAddr(ip), hasIP: true, udp: udp, process: proc})
				}
			}
		}
	}
	return out
}

// TestRuleShadowingSound: removing the rules LiveRules calls dead, and the
// redundant ones CanonicalRules drops, never changes routing; lists DiffRules
// calls equivalent route every connection the same way.
func TestRuleShadowingSound(t *testing.T) {
	conns := simConns()
	for seed := int64(0); seed < 3000; seed++ {
		r := rand.New(rand.NewSource(seed))
		var a []string
		for i, n := 0, 1+r.Intn(8); i < n; i++ {
			a = append(a, simRule(r))
		}
		live, _, _ := LiveRules(a)
		var liveLines []string
		for _, x := range live {
			liveLines = append(liveLines, x.Raw)
		}
		blocks, _ := CanonicalRules(a)
		canon := flatten(blocks)
		for _, c := range conns {
			want := simRoute(a, c)
			if got := simRoute(liveLines, c); got != want {
				t.Fatalf("seed %d: live rules route %+v to %s, full list to %s\nrules: %q\nlive: %q", seed, c, got, want, a, liveLines)
			}
			if got := simRoute(canon, c); got != want {
				t.Fatalf("seed %d: canonical rules route %+v to %s, full list to %s\nrules: %q\ncanon: %q", seed, c, got, want, a, canon)
			}
		}
		// A shuffled / edited copy.
		b := append([]string(nil), a...)
		switch r.Intn(3) {
		case 0:
			r.Shuffle(len(b), func(i, j int) { b[i], b[j] = b[j], b[i] })
		case 1:
			b = append(b, simRule(r))
		default:
			i := r.Intn(len(b))
			b = append(b[:i:i], append([]string{simRule(r)}, b[i:]...)...)
		}
		if DiffRules(a, b).Equivalent {
			for _, c := range conns {
				if ra, rb := simRoute(a, c), simRoute(b, c); ra != rb {
					t.Fatalf("seed %d: DiffRules says equivalent, but %+v goes to %s vs %s\na: %q\nb: %q", seed, c, ra, rb, a, b)
				}
			}
		}
	}
}

func TestSimSanity(t *testing.T) {
	// The model must see the cases the fixes are about.
	c := simConn{host: "cnn.com", ip: netip.MustParseAddr("8.8.8.8")}
	if simRoute([]string{"DOMAIN-KEYWORD,.cn,REJECT", "MATCH,PROXY"}, c) != "PROXY" {
		t.Fatal("keyword .cn matched cnn.com")
	}
	u := simConn{host: "a.com", ip: netip.MustParseAddr("8.8.8.8"), udp: true}
	if got := simRoute([]string{"DOMAIN-SUFFIX,a.com,NOUDP", "DOMAIN,a.com,REJECT"}, u); got != "REJECT" {
		t.Fatal(fmt.Sprint("udp fallthrough: ", got))
	}
}
