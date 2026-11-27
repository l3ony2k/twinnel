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
// "AND,((DOMAIN,a),(NETWORK,UDP)),REJECT" the way mihomo does
// (rules/common.ParseRulePayload): fields are split on every comma and
// trimmed; for logical rules, SUB-RULE and the regex types the target is the
// last field and the payload is everything in between (so a regex may contain
// commas); other types take payload, target, then options.
func ParseRule(line string) Rule {
	r := Rule{Raw: line}
	f := strings.Split(strings.TrimSpace(line), ",")
	for i := range f {
		f[i] = strings.TrimSpace(f[i])
	}
	r.Type = strings.ToUpper(f[0])
	if r.Type == "MATCH" || r.Type == "FINAL" {
		r.Type = "MATCH"
		if len(f) > 1 {
			r.Target = f[1]
		}
		return r
	}
	switch r.Type {
	case "NOT", "OR", "AND", "SUB-RULE", "DOMAIN-REGEX", "PROCESS-NAME-REGEX", "PROCESS-PATH-REGEX":
		if len(f) > 2 {
			r.Target = f[len(f)-1]
			r.Payload = strings.Join(f[1:len(f)-1], ",")
		} else if len(f) == 2 {
			r.Payload = f[1]
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
		// mihomo lower-cases these and nothing else: a leading or trailing
		// dot is part of the pattern (".example.com" as a suffix matches
		// nothing), so it must not be trimmed here.
		r.Payload = strings.ToLower(r.Payload)
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

// terminal reports whether a rule with this target always ends rule
// matching when it matches. mihomo goes on to the next rule when the target
// is (or a group currently selects) PASS, when the target does not support
// UDP and the connection is UDP, and for SUB-RULE when no sub-rule matches.
// Only the built-in DIRECT, REJECT and REJECT-DROP are known to stop.
func terminal(r Rule) bool {
	if r.Type == "SUB-RULE" {
		return false
	}
	switch r.Target {
	case "DIRECT", "REJECT", "REJECT-DROP":
		return true
	}
	return false
}

// prule is a Rule with what the cover checks need computed once: its
// matcher, its canonical line, its parsed prefix and its option flags.
type prule struct {
	Rule
	matcher  string
	canon    string
	prefix   netip.Prefix
	prefixOK bool // IP-CIDR with a parsable payload
	onlyNR   bool // every option is "no-resolve"
	noRes    bool
}

func prep(r Rule) *prule {
	p := &prule{Rule: r, matcher: r.Matcher(), canon: r.Canonical(), onlyNR: r.onlyNoResolve(), noRes: r.noResolve()}
	if r.Type == "IP-CIDR" {
		if pr, err := netip.ParsePrefix(r.Payload); err == nil {
			p.prefix, p.prefixOK = pr, true
		}
	}
	return p
}

// Covers reports whether later can never fire when earlier comes first: every
// connection matched by later is provably also matched by earlier, and
// earlier then ends rule matching or would let later through only in cases
// where later would let it through too (same target). It is conservative:
// false means "not provable", not "disjoint".
func Covers(earlier, later Rule) bool {
	if earlier.Target != later.Target && !terminal(earlier) {
		return false
	}
	return matchCovers(prep(earlier), prep(later))
}

// matchCovers reports whether every connection matched by later is matched
// by earlier, ignoring targets.
func matchCovers(earlier, later *prule) bool {
	if earlier.Type == "SUB-RULE" || later.Type == "SUB-RULE" {
		// A SUB-RULE "matches" only when one of its sub-rules does.
		return earlier.Type == later.Type && earlier.Target == later.Target && earlier.matcher == later.matcher
	}
	if earlier.Type == "MATCH" {
		return true
	}
	if earlier.matcher == later.matcher {
		return true
	}
	if !earlier.onlyNR || !later.onlyNR {
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
		if earlier.noRes && !later.noRes {
			return false
		}
		pe, pl := earlier.prefix, later.prefix
		return earlier.prefixOK && later.prefixOK && pe.Addr().Is4() == pl.Addr().Is4() &&
			pe.Bits() <= pl.Bits() && pe.Contains(pl.Addr())
	}
	return false
}

func domainUnder(d, suffix string) bool {
	return d == suffix || strings.HasSuffix(d, "."+suffix)
}

// suffixesOf calls f with d and every s such that d ends in "."+s, longest
// first, until f returns false. These are all the DOMAIN-SUFFIX payloads
// d is under (domainUnder).
func suffixesOf(d string, f func(string) bool) {
	for {
		if !f(d) {
			return
		}
		_, rest, ok := strings.Cut(d, ".")
		if !ok {
			return
		}
		d = rest
	}
}

// coveringPrefixes calls f with every masked prefix that contains p (its
// own address family, bits 0..p.Bits()), until f returns false.
func coveringPrefixes(p netip.Prefix, f func(netip.Prefix) bool) {
	for b := 0; b <= p.Bits(); b++ {
		q, err := p.Addr().Prefix(b)
		if err == nil && !f(q) {
			return
		}
	}
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

// ruleIndex answers "does any rule seen so far match everything r matches"
// quickly, ignoring targets (see coverIndex). Rules are added in index
// order, so the first rule added for a key has the smallest index.
type ruleIndex struct {
	matcher map[string]int
	suffix  map[string]int
	// Keywords: distinct payloads in the order first seen, with that
	// first index. Only the payload decides whether a keyword covers.
	keywords []string
	kwIdx    map[string]int
	// CIDRs by masked prefix: first index of a resolving rule and of a
	// no-resolve one.
	cidr   map[netip.Prefix]int
	cidrNR map[netip.Prefix]int
	match  int
}

func newRuleIndex() *ruleIndex {
	return &ruleIndex{matcher: map[string]int{}, suffix: map[string]int{}, kwIdx: map[string]int{},
		cidr: map[netip.Prefix]int{}, cidrNR: map[netip.Prefix]int{}, match: -1}
}

func setFirst[K comparable](m map[K]int, k K, i int) bool {
	if _, ok := m[k]; ok {
		return false
	}
	m[k] = i
	return true
}

func (x *ruleIndex) add(i int, r *prule) {
	setFirst(x.matcher, r.matcher, i)
	if !r.onlyNR {
		return
	}
	switch r.Type {
	case "MATCH":
		if x.match < 0 {
			x.match = i
		}
	case "DOMAIN-SUFFIX":
		setFirst(x.suffix, r.Payload, i)
	case "DOMAIN-KEYWORD":
		if r.Payload != "" && setFirst(x.kwIdx, r.Payload, i) {
			x.keywords = append(x.keywords, r.Payload)
		}
	case "IP-CIDR":
		if !r.prefixOK {
			return
		}
		if r.noRes {
			setFirst(x.cidrNR, r.prefix, i)
		} else {
			setFirst(x.cidr, r.prefix, i)
		}
	}
}

// coveredBy returns the index of an indexed rule covering r, or -1. Which
// one, when several do: MATCH, then the same matcher, then the longest
// covering DOMAIN-SUFFIX, then the first covering keyword, then the first
// covering CIDR.
func (x *ruleIndex) coveredBy(r *prule) int {
	if r.Type == "SUB-RULE" {
		if i, ok := x.matcher[r.matcher]; ok {
			return i
		}
		return -1
	}
	if x.match >= 0 {
		return x.match
	}
	if i, ok := x.matcher[r.matcher]; ok {
		return i
	}
	if !r.onlyNR {
		return -1
	}
	found := -1
	switch r.Type {
	case "DOMAIN", "DOMAIN-SUFFIX":
		suffixesOf(r.Payload, func(d string) bool {
			if i, ok := x.suffix[d]; ok {
				found = i
				return false
			}
			return true
		})
		if found >= 0 {
			return found
		}
	}
	switch r.Type {
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD":
		for _, kw := range x.keywords {
			if strings.Contains(r.Payload, kw) {
				return x.kwIdx[kw]
			}
		}
	case "IP-CIDR":
		if !r.prefixOK {
			return -1
		}
		coveringPrefixes(r.prefix, func(q netip.Prefix) bool {
			if i, ok := x.cidr[q]; ok && (found < 0 || i < found) {
				found = i
			}
			if r.noRes {
				if i, ok := x.cidrNR[q]; ok && (found < 0 || i < found) {
					found = i
				}
			}
			return true
		})
	}
	return found
}

// coverIndex is ruleIndex made target aware, like Covers: a rule is shadowed
// by an earlier rule with a terminal target, or by one with the same target.
type coverIndex struct {
	terminal *ruleIndex
	byTarget map[string]*ruleIndex
}

func newCoverIndex() *coverIndex {
	return &coverIndex{terminal: newRuleIndex(), byTarget: map[string]*ruleIndex{}}
}

func (c *coverIndex) add(i int, r *prule) {
	if terminal(r.Rule) {
		c.terminal.add(i, r)
	}
	x := c.byTarget[r.Target]
	if x == nil {
		x = newRuleIndex()
		c.byTarget[r.Target] = x
	}
	x.add(i, r)
}

func (c *coverIndex) coveredBy(r *prule) int {
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
func LiveRules(lines []string) (live []Rule, liveIdx []int, dead []DeadRule) {
	pl, liveIdx, dead := liveRules(lines)
	live = make([]Rule, len(pl))
	for i, r := range pl {
		live[i] = r.Rule
	}
	return live, liveIdx, dead
}

func liveRules(lines []string) (live []*prule, liveIdx []int, dead []DeadRule) {
	x := newCoverIndex()
	seen := map[string]bool{}
	for i, l := range lines {
		r := prep(ParseRule(l))
		if seen[r.canon] {
			j := -1
			if t := x.byTarget[r.Target]; t != nil {
				if k, ok := t.matcher[r.matcher]; ok {
					j = k
				}
			}
			dead = append(dead, DeadRule{Index: i, Rule: l, CoveredBy: j, Duplicate: true})
			continue
		}
		seen[r.canon] = true
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

// blockIndex lists, for the rules of one same-target block, every rule
// that may cover a given one (a superset; matchCovers decides).
type blockIndex struct {
	matcher  map[string][]int
	match    []int
	suffix   map[string][]int
	keywords []string // distinct, in the order first seen
	kwPos    map[string][]int
	cidr     map[netip.Prefix][]int
}

func newBlockIndex(run []*prule) *blockIndex {
	x := &blockIndex{matcher: map[string][]int{}, suffix: map[string][]int{}, kwPos: map[string][]int{},
		cidr: map[netip.Prefix][]int{}}
	for k, r := range run {
		x.matcher[r.matcher] = append(x.matcher[r.matcher], k)
		if r.Type == "MATCH" {
			x.match = append(x.match, k)
		}
		if !r.onlyNR {
			continue
		}
		switch r.Type {
		case "DOMAIN-SUFFIX":
			x.suffix[r.Payload] = append(x.suffix[r.Payload], k)
		case "DOMAIN-KEYWORD":
			if r.Payload == "" {
				continue
			}
			if _, ok := x.kwPos[r.Payload]; !ok {
				x.keywords = append(x.keywords, r.Payload)
			}
			x.kwPos[r.Payload] = append(x.kwPos[r.Payload], k)
		case "IP-CIDR":
			if r.prefixOK {
				x.cidr[r.prefix] = append(x.cidr[r.prefix], k)
			}
		}
	}
	return x
}

// candidates calls f with the position of every rule of the block that may
// cover r (possibly r itself, possibly twice).
func (x *blockIndex) candidates(r *prule, f func(int)) {
	each := func(l []int) {
		for _, m := range l {
			f(m)
		}
	}
	each(x.matcher[r.matcher])
	if r.Type == "SUB-RULE" {
		return
	}
	each(x.match)
	if !r.onlyNR {
		return
	}
	switch r.Type {
	case "DOMAIN", "DOMAIN-SUFFIX":
		suffixesOf(r.Payload, func(d string) bool { each(x.suffix[d]); return true })
	}
	switch r.Type {
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD":
		for _, kw := range x.keywords {
			if strings.Contains(r.Payload, kw) {
				each(x.kwPos[kw])
			}
		}
	case "IP-CIDR":
		if r.prefixOK {
			coveringPrefixes(r.prefix, func(q netip.Prefix) bool { each(x.cidr[q]); return true })
		}
	}
}

// CanonicalRules turns a rule list into same-target blocks with dead rules and
// rules made redundant by another rule of the same block removed. Two lists
// with equal canonical blocks route every connection the same way.
func CanonicalRules(lines []string) ([]RuleBlock, []DeadRule) {
	blocks, _, dead := canonicalRules(lines)
	return blocks, dead
}

// canonicalRules is CanonicalRules that also returns the live rules.
func canonicalRules(lines []string) ([]RuleBlock, []*prule, []DeadRule) {
	live, liveIdx, dead := liveRules(lines)
	var blocks []RuleBlock
	for i := 0; i < len(live); {
		j := i
		// Targets are case sensitive in mihomo ("Proxy" and "proxy" are
		// different groups).
		for j < len(live) && live[j].Target == live[i].Target {
			j++
		}
		run := live[i:j]
		x := newBlockIndex(run)
		b := RuleBlock{Target: live[i].Target}
		for k, r := range run {
			// r is redundant if another rule o of the block covers it,
			// unless r covers o too and o comes later (then r is the one
			// kept). The smallest such o is reported.
			by := -1
			x.candidates(r, func(m int) {
				if m == k || by >= 0 && m >= by {
					return
				}
				o := run[m]
				if !matchCovers(o, r) {
					return
				}
				if m < k || !matchCovers(r, o) {
					by = m
				}
			})
			if by >= 0 {
				dead = append(dead, DeadRule{Index: liveIdx[i+k], Rule: r.Raw, CoveredBy: liveIdx[i+by], Redundant: true})
				continue
			}
			b.Rules = append(b.Rules, r.canon)
		}
		sort.Strings(b.Rules)
		blocks = append(blocks, b)
		i = j
	}
	sort.Slice(dead, func(a, b int) bool { return dead[a].Index < dead[b].Index })
	return blocks, live, dead
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
	ba, la, deadA := canonicalRules(a)
	bb, lb, deadB := canonicalRules(b)
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
		d.Reordered = !sameCanonical(la, lb)
		return d
	}
	d.Edits = diffLines(fa, fb)
	return d
}

func sameCanonical(a, b []*prule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].canon != b[i].canon {
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
		if a[i].Target != b[i].Target || !equalStrings(a[i].Rules, b[i].Rules) {
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
