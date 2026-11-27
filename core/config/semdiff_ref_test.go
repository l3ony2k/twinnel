package config

// The implementation of LiveRules and CanonicalRules before the indexed
// rewrite (quadratic, recomputes matchers and prefixes on every compare),
// kept as the reference for the differential tests.

import (
	"net/netip"
	"sort"
	"strings"
)

func coversRef(earlier, later Rule) bool {
	if earlier.Target != later.Target && !terminal(earlier) {
		return false
	}
	return matchCoversRef(earlier, later)
}

// matchCovers reports whether every connection matched by later is matched
// by earlier, ignoring targets.
func matchCoversRef(earlier, later Rule) bool {
	if earlier.Type == "SUB-RULE" || later.Type == "SUB-RULE" {
		// A SUB-RULE "matches" only when one of its sub-rules does.
		return earlier.Type == later.Type && earlier.Target == later.Target && earlier.Matcher() == later.Matcher()
	}
	if earlier.Type == "MATCH" {
		return true
	}
	if earlier.Matcher() == later.Matcher() {
		return true
	}
	if !earlier.onlyNoResolve() || !later.onlyNoResolve() {
		return false
	}
	switch earlier.Type {
	case "DOMAIN-SUFFIX":
		switch later.Type {
		case "DOMAIN", "DOMAIN-SUFFIX":
			return domainUnder(later.Payload, earlier.Payload)
		}
	case "DOMAIN-KEYWORD":
		switch later.Type {
		case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD":
			return earlier.Payload != "" && strings.Contains(later.Payload, earlier.Payload)
		}
	case "IP-CIDR":
		if later.Type != "IP-CIDR" {
			return false
		}
		// A resolving rule may match a domain request after DNS; a
		// no-resolve rule before it would not have seen the IP.
		if earlier.noResolve() && !later.noResolve() {
			return false
		}
		pe, err1 := netip.ParsePrefix(earlier.Payload)
		pl, err2 := netip.ParsePrefix(later.Payload)
		return err1 == nil && err2 == nil && pe.Addr().Is4() == pl.Addr().Is4() &&
			pe.Bits() <= pl.Bits() && pe.Contains(pl.Addr())
	}
	return false
}

// ruleIndexRef answers "does any rule seen so far match everything r matches"
// quickly, ignoring targets (see coverIndexRef).
type ruleIndexRef struct {
	matcher  map[string]int
	suffix   map[string]int
	keywords []Rule
	kwIdx    []int
	cidrs    []Rule
	cidrIdx  []int
	match    int
}

func newRuleIndexRef() *ruleIndexRef {
	return &ruleIndexRef{matcher: map[string]int{}, suffix: map[string]int{}, match: -1}
}

func (x *ruleIndexRef) add(i int, r Rule) {
	if _, ok := x.matcher[r.Matcher()]; !ok {
		x.matcher[r.Matcher()] = i
	}
	if !r.onlyNoResolve() {
		return
	}
	switch r.Type {
	case "MATCH":
		if x.match < 0 {
			x.match = i
		}
	case "DOMAIN-SUFFIX":
		if _, ok := x.suffix[r.Payload]; !ok {
			x.suffix[r.Payload] = i
		}
	case "DOMAIN-KEYWORD":
		x.keywords, x.kwIdx = append(x.keywords, r), append(x.kwIdx, i)
	case "IP-CIDR":
		x.cidrs, x.cidrIdx = append(x.cidrs, r), append(x.cidrIdx, i)
	}
}

// coveredBy returns the index of an indexed rule covering r, or -1.
func (x *ruleIndexRef) coveredBy(r Rule) int {
	if r.Type == "SUB-RULE" {
		if i, ok := x.matcher[r.Matcher()]; ok {
			return i
		}
		return -1
	}
	if x.match >= 0 {
		return x.match
	}
	if i, ok := x.matcher[r.Matcher()]; ok {
		return i
	}
	if !r.onlyNoResolve() {
		return -1
	}
	switch r.Type {
	case "DOMAIN", "DOMAIN-SUFFIX":
		d := r.Payload
		for {
			if i, ok := x.suffix[d]; ok {
				return i
			}
			_, rest, ok := strings.Cut(d, ".")
			if !ok {
				break
			}
			d = rest
		}
	}
	switch r.Type {
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD":
		for k, kw := range x.keywords {
			if matchCoversRef(kw, r) {
				return x.kwIdx[k]
			}
		}
	case "IP-CIDR":
		for k, c := range x.cidrs {
			if matchCoversRef(c, r) {
				return x.cidrIdx[k]
			}
		}
	}
	return -1
}

// coverIndexRef is ruleIndexRef made target aware, like Covers: a rule is shadowed
// by an earlier rule with a terminal target, or by one with the same target.
type coverIndexRef struct {
	terminal *ruleIndexRef
	byTarget map[string]*ruleIndexRef
}

func newCoverIndexRef() *coverIndexRef {
	return &coverIndexRef{terminal: newRuleIndexRef(), byTarget: map[string]*ruleIndexRef{}}
}

func (c *coverIndexRef) add(i int, r Rule) {
	if terminal(r) {
		c.terminal.add(i, r)
	}
	x := c.byTarget[r.Target]
	if x == nil {
		x = newRuleIndexRef()
		c.byTarget[r.Target] = x
	}
	x.add(i, r)
}

func (c *coverIndexRef) coveredBy(r Rule) int {
	best := c.terminal.coveredBy(r)
	if x := c.byTarget[r.Target]; x != nil {
		if j := x.coveredBy(r); j >= 0 && (best < 0 || j < best) {
			best = j
		}
	}
	return best
}

// LiveRules returns the rules that can fire under first-match semantics, in
// order, and the dead ones (exact duplicates and rules shadowed by an
// earlier rule whatever its target).
func liveRulesRef(lines []string) (live []Rule, liveIdx []int, dead []DeadRule) {
	x := newCoverIndexRef()
	seen := map[string]bool{}
	for i, l := range lines {
		r := ParseRule(l)
		c := r.Canonical()
		if seen[c] {
			j := -1
			if t := x.byTarget[r.Target]; t != nil {
				if k, ok := t.matcher[r.Matcher()]; ok {
					j = k
				}
			}
			dead = append(dead, DeadRule{Index: i, Rule: l, CoveredBy: j, Duplicate: true})
			continue
		}
		seen[c] = true
		if j := x.coveredBy(r); j >= 0 {
			dead = append(dead, DeadRule{Index: i, Rule: l, CoveredBy: j})
			continue
		}
		x.add(i, r)
		live = append(live, r)
		liveIdx = append(liveIdx, i)
	}
	return live, liveIdx, dead
}

// CanonicalRules turns a rule list into same-target blocks with dead rules and
// rules made redundant by another rule of the same block removed. Two lists
// with equal canonical blocks route every connection the same way.
func canonicalRulesRef(lines []string) ([]RuleBlock, []DeadRule) {
	live, liveIdx, dead := liveRulesRef(lines)
	var blocks []RuleBlock
	for i := 0; i < len(live); {
		j := i
		// Targets are case sensitive in mihomo ("Proxy" and "proxy" are
		// different groups).
		for j < len(live) && live[j].Target == live[i].Target {
			j++
		}
		run := live[i:j]
		b := RuleBlock{Target: live[i].Target}
		for k, r := range run {
			redundant := false
			for m, o := range run {
				if m == k || !coversRef(o, r) {
					continue
				}
				if !coversRef(r, o) || m < k {
					redundant = true
					dead = append(dead, DeadRule{Index: liveIdx[i+k], Rule: r.Raw, CoveredBy: liveIdx[i+m], Redundant: true})
					break
				}
			}
			if !redundant {
				b.Rules = append(b.Rules, r.Canonical())
			}
		}
		sort.Strings(b.Rules)
		blocks = append(blocks, b)
		i = j
	}
	sort.Slice(dead, func(a, b int) bool { return dead[a].Index < dead[b].Index })
	return blocks, dead
}
