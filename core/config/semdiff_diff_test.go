package config

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/l3ony2k/duet/core/internal/rulegen"
)

// diffRule draws from a pool with heavy overlap so that suffix, keyword,
// CIDR and option interactions all occur.
func diffRule(r *rand.Rand) string {
	targets := []string{"DIRECT", "REJECT", "PROXY", "Proxy", "REJECT-DROP", "sub1"}
	t := targets[r.Intn(len(targets))]
	doms := []string{"a.com", "x.a.com", "y.x.a.com", "b.com", "com", ".com", "a..com", "", "A.Com", "x.cn", "cn", "foo."}
	kws := []string{"a", "cn", ".c", "x.a", "", "om", "b.co"}
	nets := []string{"10.0.0.0/8", "10.1.0.0/16", "10.1.2.0/24", "10.1.2.3/32", "0.0.0.0/0", "10.1.2.3/8",
		"::/0", "2001:db8::/32", "2001:db8:1::/48", "::ffff:10.0.0.0/104", "::ffff:0:0/96", "bad/99", "10.0.0.1"}
	opts := []string{"", ",no-resolve", ",no-resolve,no-resolve", ",src", ",NO-RESOLVE", ",src,no-resolve"}
	o := opts[r.Intn(len(opts))]
	d := doms[r.Intn(len(doms))]
	switch r.Intn(12) {
	case 0, 1:
		return "DOMAIN," + d + "," + t + o
	case 2, 3, 4:
		return "DOMAIN-SUFFIX," + d + "," + t + o
	case 5:
		return "DOMAIN-KEYWORD," + kws[r.Intn(len(kws))] + "," + t + o
	case 6, 7:
		typ := []string{"IP-CIDR", "IP-CIDR6", "SRC-IP-CIDR"}[r.Intn(3)]
		return typ + "," + nets[r.Intn(len(nets))] + "," + t + o
	case 8:
		return "GEOIP,CN," + t + o
	case 9:
		return "SUB-RULE,(NETWORK,tcp)," + t
	case 10:
		return "AND,((DOMAIN," + d + "),(NETWORK,UDP))," + t
	}
	return "MATCH," + t
}

func checkSame(t *testing.T, name string, lines []string) {
	t.Helper()
	l1, i1, d1 := LiveRules(lines)
	l2, i2, d2 := liveRulesRef(lines)
	if !reflect.DeepEqual(l1, l2) || !reflect.DeepEqual(i1, i2) || !reflect.DeepEqual(d1, d2) {
		t.Fatalf("%s: LiveRules differs from the reference\nrules: %q\nnew dead: %+v\nref dead: %+v", name, lines, d1, d2)
	}
	b1, e1 := CanonicalRules(lines)
	b2, e2 := canonicalRulesRef(lines)
	if !reflect.DeepEqual(b1, b2) || !reflect.DeepEqual(e1, e2) {
		t.Fatalf("%s: CanonicalRules differs from the reference\nrules: %q\nnew: %+v %+v\nref: %+v %+v", name, lines, b1, e1, b2, e2)
	}
}

// TestRulesMatchReference: the indexed LiveRules and CanonicalRules return
// exactly what the old quadratic implementation did.
func TestRulesMatchReference(t *testing.T) {
	for seed := int64(0); seed < 5000; seed++ {
		r := rand.New(rand.NewSource(seed))
		var a []string
		for i, n := 0, 1+r.Intn(30); i < n; i++ {
			a = append(a, diffRule(r))
		}
		checkSame(t, fmt.Sprint("seed ", seed), a)
	}
	for seed := int64(0); seed < 3; seed++ {
		checkSame(t, fmt.Sprint("mixed ", seed), rulegen.Rules(seed, 1500))
	}
	// One large block, as in real configs (the reference is quadratic).
	small := []rulegen.Block{{Target: "DIRECT", Suffix: 300, Domain: 100, Keyword: 30, CIDR: 800, CIDR6: 30, Other: 10},
		{Target: "PROXY", Suffix: 200, Keyword: 5, CIDR: 20}}
	checkSame(t, "shaped", rulegen.Shaped(7, small))
}

func TestDiffRulesMatchesReference(t *testing.T) {
	for seed := int64(0); seed < 2000; seed++ {
		r := rand.New(rand.NewSource(seed))
		var a []string
		for i, n := 0, 1+r.Intn(20); i < n; i++ {
			a = append(a, diffRule(r))
		}
		b := append([]string{diffRule(r)}, a...)
		if r.Intn(2) == 0 {
			r.Shuffle(len(b), func(i, j int) { b[i], b[j] = b[j], b[i] })
		}
		got := DiffRules(a, b)
		ba, da := canonicalRulesRef(a)
		bb, db := canonicalRulesRef(b)
		la, _, _ := liveRulesRef(a)
		lb, _, _ := liveRulesRef(b)
		eq := blocksEqual(ba, bb)
		reord := false
		if eq {
			reord = len(la) != len(lb)
			for i := 0; !reord && i < len(la); i++ {
				reord = la[i].Canonical() != lb[i].Canonical()
			}
		}
		if got.Equivalent != eq || got.Reordered != reord || got.LiveA != len(flatten(ba)) || got.LiveB != len(flatten(bb)) ||
			got.DupA+len(got.DeadA) != len(da) || got.DupB+len(got.DeadB) != len(db) {
			t.Fatalf("seed %d: DiffRules %+v differs from the reference\na: %q\nb: %q", seed, got, a, b)
		}
	}
}
