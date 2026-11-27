// Package rulegen generates synthetic mihomo rule lists for benchmarks and
// differential tests. The lists have the shape of real subscription rules
// (long same-target blocks of DOMAIN-SUFFIX / DOMAIN / IP-CIDR, a few
// keywords, GEOIP, RULE-SET and PROCESS-NAME, MATCH at the end) but contain
// only made-up names: never real configs.
package rulegen

import (
	"fmt"
	"math/rand"
	"strings"
)

var (
	tlds    = []string{"com", "net", "org", "cn", "io", "jp", "co", "dev", "app", "tv"}
	targets = []string{"DIRECT", "PROXY", "REJECT", "Streaming", "AI", "Apple", "Games", "REJECT-DROP"}
	sylls   = []string{"ka", "lo", "mi", "ra", "zen", "tor", "vel", "qua", "nex", "ori", "bel", "dax", "fin", "gro", "hul", "ix"}
)

func label(r *rand.Rand) string {
	var b strings.Builder
	for i, n := 0, 2+r.Intn(3); i < n; i++ {
		b.WriteString(sylls[r.Intn(len(sylls))])
	}
	return b.String()
}

// Rules returns n synthetic rule lines (the last one MATCH) from seed.
func Rules(seed int64, n int) []string {
	r := rand.New(rand.NewSource(seed))
	var out, domains []string
	newDomain := func() string {
		// Realistic overlap: a subdomain of an earlier domain, or the
		// same name under another TLD.
		if len(domains) > 0 && r.Intn(6) == 0 {
			return label(r) + "." + domains[r.Intn(len(domains))]
		}
		d := label(r) + "." + tlds[r.Intn(len(tlds))]
		domains = append(domains, d)
		return d
	}
	v4 := func() string {
		bits := 8 + r.Intn(25)
		return fmt.Sprintf("%d.%d.%d.%d/%d", 1+r.Intn(223), r.Intn(256), r.Intn(256), r.Intn(256), bits)
	}
	v6 := func() string {
		return fmt.Sprintf("2001:%x:%x::/%d", r.Intn(0x10000), r.Intn(0x10000), 24+r.Intn(41))
	}
	for len(out) < n-1 {
		t := targets[r.Intn(len(targets))]
		size := 1 + r.Intn(40)
		if r.Intn(5) == 0 {
			size = 200 + r.Intn(2500) // a big imported list
		}
		kind := r.Intn(10)
		for i := 0; i < size && len(out) < n-1; i++ {
			var line string
			switch k := r.Intn(100); {
			case kind < 6 && k < 70 || kind >= 6 && k < 20:
				line = "DOMAIN-SUFFIX," + newDomain() + "," + t
			case kind < 6 && k < 88 || kind >= 6 && k < 30:
				line = "DOMAIN," + newDomain() + "," + t
			case k < 90:
				line = "DOMAIN-KEYWORD," + label(r) + "," + t
			case kind >= 6 && k < 80:
				line = "IP-CIDR," + v4() + "," + t
				if r.Intn(2) == 0 {
					line += ",no-resolve"
				}
			case kind >= 6 && k < 92:
				line = "IP-CIDR6," + v6() + "," + t + ",no-resolve"
			case k < 94:
				line = "GEOIP," + strings.ToUpper(tlds[r.Intn(len(tlds))]) + "," + t
			case k < 96:
				line = "RULE-SET," + label(r) + "," + t
			case k < 98:
				line = "PROCESS-NAME," + label(r) + "," + t
			default:
				// An exact repeat of an earlier line.
				if len(out) > 0 {
					line = out[r.Intn(len(out))]
				} else {
					line = "DOMAIN," + newDomain() + "," + t
				}
			}
			out = append(out, line)
		}
	}
	return append(out, "MATCH,PROXY")
}

// Block describes one same-target run for Shaped: how many rules of each
// kind it holds (shuffled together).
type Block struct {
	Target                                      string
	Suffix, Domain, Keyword, CIDR, CIDR6, Other int
}

// TypicalBlocks is the shape of a large personal config: one huge
// DIRECT block holding a country IP list plus domain lists, a few
// medium domain lists, small hand-written blocks, MATCH last (about 8500
// rules in 16 blocks).
var TypicalBlocks = []Block{
	{Target: "DIRECT", Suffix: 20, Domain: 10, CIDR: 4, Other: 3},
	{Target: "REJECT", Suffix: 180, CIDR6: 1, Keyword: 1, CIDR: 2},
	{Target: "PROXY", Suffix: 25, Domain: 11},
	{Target: "AI", Suffix: 24, Domain: 7, CIDR: 2, Keyword: 1},
	{Target: "Apple", Suffix: 5, Domain: 14, CIDR6: 2, Other: 2},
	{Target: "Streaming", Suffix: 82, Domain: 87, CIDR: 23},
	{Target: "PROXY", Suffix: 684, Keyword: 5, Other: 5, CIDR: 4},
	{Target: "Games", Suffix: 10, Domain: 3},
	{Target: "PROXY", Suffix: 544, CIDR: 23, Keyword: 3, Other: 1},
	{Target: "DIRECT", Suffix: 8, CIDR: 3},
	{Target: "REJECT-DROP", Domain: 5, Suffix: 4},
	{Target: "DIRECT", Suffix: 739, Keyword: 250, CIDR: 5637, CIDR6: 70, Domain: 20, Other: 8},
	{Target: "PROXY", Suffix: 6, Domain: 4},
	{Target: "Streaming", Suffix: 12, Keyword: 2},
	{Target: "DIRECT", Other: 2},
}

// Shaped returns a rule list with the given block shape (MATCH appended).
func Shaped(seed int64, blocks []Block) []string {
	r := rand.New(rand.NewSource(seed))
	var out, domains []string
	newDomain := func() string {
		if len(domains) > 0 && r.Intn(6) == 0 {
			return label(r) + "." + domains[r.Intn(len(domains))]
		}
		d := label(r) + "." + tlds[r.Intn(len(tlds))]
		domains = append(domains, d)
		return d
	}
	var nets []string
	v4 := func() string {
		// Country IP lists are many adjacent /13–/24 nets with some
		// nesting.
		if len(nets) > 0 && r.Intn(8) == 0 {
			var a, b, c, d, bits int
			fmt.Sscanf(nets[r.Intn(len(nets))], "%d.%d.%d.%d/%d", &a, &b, &c, &d, &bits)
			if bits < 24 {
				return fmt.Sprintf("%d.%d.%d.0/24", a, b, c)
			}
		}
		n := fmt.Sprintf("%d.%d.%d.0/%d", 1+r.Intn(223), r.Intn(256), r.Intn(256), 13+r.Intn(12))
		nets = append(nets, n)
		return n
	}
	for _, b := range blocks {
		var run []string
		add := func(n int, f func() string) {
			for i := 0; i < n; i++ {
				run = append(run, f())
			}
		}
		t := b.Target
		add(b.Suffix, func() string { return "DOMAIN-SUFFIX," + newDomain() + "," + t })
		add(b.Domain, func() string { return "DOMAIN," + newDomain() + "," + t })
		add(b.Keyword, func() string { return "DOMAIN-KEYWORD," + label(r) + "," + t })
		add(b.CIDR, func() string { return "IP-CIDR," + v4() + "," + t + ",no-resolve" })
		add(b.CIDR6, func() string {
			return fmt.Sprintf("IP-CIDR6,2001:%x:%x::/%d,%s,no-resolve", r.Intn(0x10000), r.Intn(0x10000), 24+r.Intn(41), t)
		})
		add(b.Other, func() string {
			switch r.Intn(4) {
			case 0:
				return "PROCESS-NAME," + label(r) + "," + t
			case 1:
				return "IP-ASN," + fmt.Sprint(1000+r.Intn(60000)) + "," + t + ",no-resolve"
			case 2:
				return "GEOIP,CN," + t
			}
			return "RULE-SET," + label(r) + "," + t
		})
		// Lists are sorted by kind in real files, but mix a little.
		for i := range run {
			if r.Intn(10) == 0 {
				j := r.Intn(len(run))
				run[i], run[j] = run[j], run[i]
			}
		}
		out = append(out, run...)
	}
	return append(out, "MATCH,PROXY")
}
