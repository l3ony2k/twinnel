package config

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// Rule is one parsed mihomo rule line.
type Rule struct {
	Raw     string   // the line as written
	Type    string   // upper case, e.g. "DOMAIN-SUFFIX"; IP-CIDR6 is folded into IP-CIDR
	Payload string   // normalized (domains lower case, prefixes masked)
	Target  string   // proxy, group, DIRECT, REJECT, ... (for SUB-RULE the sub-rule name)
	Opts    []string // e.g. "no-resolve", lower case
}

// ParseRule parses a rule line such as "DOMAIN-SUFFIX,example.com,DIRECT" or
// "AND,((DOMAIN,a),(NETWORK,UDP)),REJECT". Unknown shapes are kept with their
// fields as given.
func ParseRule(line string) Rule {
	r := Rule{Raw: line}
	f := splitTopLevel(strings.TrimSpace(line))
	if len(f) == 0 {
		return r
	}
	r.Type = strings.ToUpper(f[0])
	if r.Type == "MATCH" || r.Type == "FINAL" {
		r.Type = "MATCH"
		if len(f) > 1 {
			r.Target = f[1]
		}
		return r
	}
	if len(f) > 1 {
		r.Payload = f[1]
	}
	if len(f) > 2 {
		r.Target = f[2]
	}
	if len(f) > 3 {
		for _, o := range f[3:] {
			r.Opts = append(r.Opts, strings.ToLower(o))
		}
	}
	switch r.Type {
	case "IP-CIDR6":
		r.Type = "IP-CIDR"
		fallthrough
	case "IP-CIDR", "SRC-IP-CIDR":
		if p, err := netip.ParsePrefix(r.Payload); err == nil {
			r.Payload = p.Masked().String()
		}
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD":
		r.Payload = strings.ToLower(strings.Trim(r.Payload, "."))
	}
	sort.Strings(r.Opts)
	return r
}

// Matcher is the rule without its target: two rules with the same matcher
// match the same connections.
func (r Rule) Matcher() string {
	s := r.Type + "," + r.Payload
	if len(r.Opts) > 0 {
		s += "," + strings.Join(r.Opts, ",")
	}
	return s
}

// Canonical is the normalized line (type, payload, target, options).
func (r Rule) Canonical() string {
	if r.Type == "MATCH" {
		return "MATCH," + r.Target
	}
	s := r.Type + "," + r.Payload + "," + r.Target
	if len(r.Opts) > 0 {
		s += "," + strings.Join(r.Opts, ",")
	}
	return s
}

func (r Rule) noResolve() bool {
	for _, o := range r.Opts {
		if o == "no-resolve" {
			return true
		}
	}
	return false
}

// onlyNoResolve reports whether every option is "no-resolve" (no "src" and
// the like, which change what is matched).
func (r Rule) onlyNoResolve() bool {
	for _, o := range r.Opts {
		if o != "no-resolve" {
			return false
		}
	}
	return true
}

// splitTopLevel splits s on commas outside parentheses and trims each field.
func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// Covers reports whether every connection matched by later is provably also
// matched by earlier, so later can never fire when earlier comes first.
// It is conservative: false means "not provable", not "disjoint".
func Covers(earlier, later Rule) bool {
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

func domainUnder(d, suffix string) bool {
	return d == suffix || strings.HasSuffix(d, "."+suffix)
}

// DeadRule is a rule that can never fire.
type DeadRule struct {
	Index     int    `json:"index"`               // position in the original list
	Rule      string `json:"rule"`                // the line
	CoveredBy int    `json:"covered_by"`          // index of the rule that shadows it (-1 for exact duplicates of a removed line)
	Duplicate bool   `json:"duplicate,omitempty"` // exact repeat of an earlier line
	// Redundant: covered by another rule of the same same-target block
	// (possibly a later one), so removing it changes nothing.
	Redundant bool `json:"redundant,omitempty"`
}

// ruleIndex answers "is r covered by any rule seen so far" quickly.
type ruleIndex struct {
	matcher  map[string]int
	suffix   map[string]int
	keywords []Rule
	kwIdx    []int
	cidrs    []Rule
	cidrIdx  []int
	match    int
}

func newRuleIndex() *ruleIndex {
	return &ruleIndex{matcher: map[string]int{}, suffix: map[string]int{}, match: -1}
}

func (x *ruleIndex) add(i int, r Rule) {
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
func (x *ruleIndex) coveredBy(r Rule) int {
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
			if Covers(kw, r) {
				return x.kwIdx[k]
			}
		}
	case "IP-CIDR":
		for k, c := range x.cidrs {
			if Covers(c, r) {
				return x.cidrIdx[k]
			}
		}
	}
	return -1
}

// LiveRules returns the rules that can fire under first-match semantics, in
// order, and the dead ones (exact duplicates and rules shadowed by an
// earlier rule whatever its target).
func LiveRules(lines []string) (live []Rule, liveIdx []int, dead []DeadRule) {
	x := newRuleIndex()
	seen := map[string]bool{}
	for i, l := range lines {
		r := ParseRule(l)
		c := r.Canonical()
		if seen[c] {
			j, ok := x.matcher[r.Matcher()]
			if !ok {
				j = -1
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

// RuleBlock is a maximal run of consecutive live rules with the same target.
// Inside a block order does not matter: whichever rule matches first, the
// result is the same target.
type RuleBlock struct {
	Target string
	Rules  []string // canonical lines, sorted
}

// CanonicalRules turns a rule list into same-target blocks with dead rules and
// rules made redundant by another rule of the same block removed. Two lists
// with equal canonical blocks route every connection the same way.
func CanonicalRules(lines []string) ([]RuleBlock, []DeadRule) {
	live, liveIdx, dead := LiveRules(lines)
	var blocks []RuleBlock
	for i := 0; i < len(live); {
		j := i
		for j < len(live) && strings.EqualFold(live[j].Target, live[i].Target) {
			j++
		}
		run := live[i:j]
		b := RuleBlock{Target: live[i].Target}
		for k, r := range run {
			redundant := false
			for m, o := range run {
				if m == k || !Covers(o, r) {
					continue
				}
				if !Covers(r, o) || m < k {
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

// RuleEdit is one line of a rule diff.
type RuleEdit struct {
	Op   string `json:"op"` // "+" (only in B) or "-" (only in A)
	Rule string `json:"rule"`
}

func (e RuleEdit) String() string { return e.Op + " " + e.Rule }

// RuleDiff compares two rule lists by meaning.
type RuleDiff struct {
	CountA int        `json:"count_a"`
	CountB int        `json:"count_b"`
	DupA   int        `json:"dup_a"` // exact duplicate lines
	DupB   int        `json:"dup_b"`
	DeadA  []DeadRule `json:"dead_a,omitempty"` // shadowed or redundant rules (duplicates excluded)
	DeadB  []DeadRule `json:"dead_b,omitempty"`
	LiveA  int        `json:"live_a"` // rules that can fire
	LiveB  int        `json:"live_b"`
	// Identical: the lists are equal as written.
	Identical bool `json:"identical"`
	// Equivalent: after removing dead rules and ignoring the order inside
	// same-target blocks, the lists are equal.
	Equivalent bool `json:"equivalent"`
	// Reordered: equivalent, but the live rules are in a different order.
	Reordered bool       `json:"reordered"`
	Edits     []RuleEdit `json:"edits,omitempty"` // diff of the canonical forms (empty when equivalent)
}

func flatten(blocks []RuleBlock) []string {
	var out []string
	for _, b := range blocks {
		out = append(out, b.Rules...)
	}
	return out
}

// DiffRules compares rule lists a and b under first-match semantics.
func DiffRules(a, b []string) RuleDiff {
	d := RuleDiff{CountA: len(a), CountB: len(b), Identical: equalStrings(a, b)}
	ba, deadA := CanonicalRules(a)
	bb, deadB := CanonicalRules(b)
	split := func(dead []DeadRule) (dups int, rest []DeadRule) {
		for _, x := range dead {
			if x.Duplicate {
				dups++
			} else {
				rest = append(rest, x)
			}
		}
		return
	}
	d.DupA, d.DeadA = split(deadA)
	d.DupB, d.DeadB = split(deadB)
	fa, fb := flatten(ba), flatten(bb)
	d.LiveA, d.LiveB = len(fa), len(fb)
	d.Equivalent = blocksEqual(ba, bb)
	if d.Equivalent {
		la, _, _ := LiveRules(a)
		lb, _, _ := LiveRules(b)
		d.Reordered = !sameCanonical(la, lb)
		return d
	}
	d.Edits = diffLines(fa, fb)
	return d
}

func sameCanonical(a, b []Rule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Canonical() != b[i].Canonical() {
			return false
		}
	}
	return true
}

func blocksEqual(a, b []RuleBlock) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i].Target, b[i].Target) || !equalStrings(a[i].Rules, b[i].Rules) {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// maxLCSCells bounds the LCS table; larger middles fall back to set diff.
const maxLCSCells = 4_000_000

// diffLines is an LCS diff after stripping the common prefix and suffix.
func diffLines(a, b []string) []RuleEdit {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	a, b = a[p:len(a)-s], b[p:len(b)-s]
	n, m := len(a), len(b)
	var out []RuleEdit
	if (n+1)*(m+1) > maxLCSCells {
		inB := map[string]bool{}
		for _, x := range b {
			inB[x] = true
		}
		inA := map[string]bool{}
		for _, x := range a {
			inA[x] = true
			if !inB[x] {
				out = append(out, RuleEdit{"-", x})
			}
		}
		for _, x := range b {
			if !inA[x] {
				out = append(out, RuleEdit{"+", x})
			}
		}
		if len(out) == 0 {
			out = append(out, RuleEdit{"~", fmt.Sprintf("%d rules in a different order", n)})
		}
		return out
	}
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				dp[i][j] = dp[i+1][j+1] + 1
			case dp[i+1][j] >= dp[i][j+1]:
				dp[i][j] = dp[i+1][j]
			default:
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			i++
			j++
		case j < m && (i == n || dp[i][j+1] >= dp[i+1][j]):
			out = append(out, RuleEdit{"+", b[j]})
			j++
		default:
			out = append(out, RuleEdit{"-", a[i]})
			i++
		}
	}
	return out
}
