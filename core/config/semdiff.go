package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SettingChange is one difference outside the data sections.
type SettingChange struct {
	Path string `json:"path"`
	// Kind is "added" (only in B), "removed" (only in A), "changed", or
	// "reordered" (same set, different order: no effect).
	Kind string `json:"kind"`
	A    string `json:"a,omitempty"` // redacted rendering
	B    string `json:"b,omitempty"`
}

func (c SettingChange) String() string {
	switch c.Kind {
	case "added":
		return fmt.Sprintf("%s: added %s", c.Path, c.B)
	case "removed":
		return fmt.Sprintf("%s: removed (was %s)", c.Path, c.A)
	case "reordered":
		return fmt.Sprintf("%s: same entries, different order", c.Path)
	}
	return fmt.Sprintf("%s: %s → %s", c.Path, c.A, c.B)
}

// Effective reports whether the change can alter behavior.
func (c SettingChange) Effective() bool { return c.Kind != "reordered" }

// NamedDiff compares a list of named items (proxies, groups, providers).
type NamedDiff struct {
	CountA    int      `json:"count_a"`
	CountB    int      `json:"count_b"`
	OnlyA     []string `json:"only_a,omitempty"`
	OnlyB     []string `json:"only_b,omitempty"`
	Changed   []string `json:"changed,omitempty"`
	Reordered bool     `json:"reordered,omitempty"`
	// OrderMatters marks a reordering as a behaviour change. Set for
	// proxies when a group in either config uses include-all or
	// include-all-proxies: such a group takes its members in the order of
	// the proxies list, and the order decides fallback priority, the
	// load-balance hash and round-robin order, url-test ties (tolerance) and
	// the default choice of a select group.
	OrderMatters bool `json:"order_matters,omitempty"`
	// Details holds per-item field differences; never set for proxies and
	// providers (their fields hold credentials).
	Details []SettingChange `json:"details,omitempty"`
	// Hashes maps name → short content hash of B's items (A's when only in
	// A), for proxies and providers only.
	Hashes map[string]string `json:"hashes,omitempty"`
}

// Same reports whether the items are equal: same names and contents, and
// the same order where the order matters.
func (n NamedDiff) Same() bool {
	return len(n.OnlyA) == 0 && len(n.OnlyB) == 0 && len(n.Changed) == 0 &&
		!(n.Reordered && n.OrderMatters)
}

// SemDiff is the semantic difference between config A and config B.
type SemDiff struct {
	Settings       []SettingChange `json:"settings,omitempty"`
	Proxies        NamedDiff       `json:"proxies"`
	ProxyProviders NamedDiff       `json:"proxy_providers"`
	ProxyGroups    NamedDiff       `json:"proxy_groups"`
	RuleProviders  NamedDiff       `json:"rule_providers"`
	Rules          RuleDiff        `json:"rules"`
	SubRules       []string        `json:"sub_rules,omitempty"` // names of sub-rule sets that differ
}

// Equivalent reports whether A and B behave the same: no effective setting
// change, same proxies, providers and groups, and rules equivalent under
// first-match semantics.
func (d *SemDiff) Equivalent() bool {
	for _, c := range d.Settings {
		if c.Effective() {
			return false
		}
	}
	return d.Proxies.Same() && d.ProxyProviders.Same() && d.ProxyGroups.Same() &&
		d.RuleProviders.Same() && d.Rules.Equivalent && len(d.SubRules) == 0
}

// EffectiveSettings returns the setting changes that alter behavior.
func (d *SemDiff) EffectiveSettings() []SettingChange {
	var out []SettingChange
	for _, c := range d.Settings {
		if c.Effective() {
			out = append(out, c)
		}
	}
	return out
}

// dataSections are compared item by item, not as settings.
var dataSections = map[string]bool{
	"proxies": true, "proxy-providers": true, "proxy-groups": true,
	"rule-providers": true, "rules": true, "sub-rules": true,
}

// setPaths are lists whose order has no effect.
var setPaths = map[string]bool{
	"dns.fake-ip-filter":          true,
	"dns.default-nameserver":      true,
	"dns.nameserver":              true,
	"dns.fallback":                true,
	"dns.proxy-server-nameserver": true,
	"dns.direct-nameserver":       true,
	"tun.route-exclude-address":   true,
	"tun.route-include-address":   true,
	"tun.dns-hijack":              true,
	"tun.include-interface":       true,
	"tun.exclude-interface":       true,
	"skip-auth-prefixes":          true,
	"lan-allowed-ips":             true,
	"lan-disallowed-ips":          true,
}

// caseInsensitive are scalar settings mihomo reads case-insensitively.
var caseInsensitive = map[string]bool{
	"mode": true, "log-level": true, "dns.enhanced-mode": true, "tun.stack": true,
	"find-process-mode": true,
}

// SemanticDiff compares two mihomo configs by meaning. It never renders proxy
// or provider contents, and redacts credentials in settings.
func SemanticDiff(a, b []byte) (*SemDiff, error) {
	ma, err := decodeTop(a)
	if err != nil {
		return nil, fmt.Errorf("config A: %w", err)
	}
	mb, err := decodeTop(b)
	if err != nil {
		return nil, fmt.Errorf("config B: %w", err)
	}
	d := &SemDiff{}
	settingsA, settingsB := map[string]any{}, map[string]any{}
	for k, v := range ma {
		if !dataSections[k] {
			settingsA[k] = v
		}
	}
	for k, v := range mb {
		if !dataSections[k] {
			settingsB[k] = v
		}
	}
	sets := setPaths
	if fakeIPRuleMode(ma) || fakeIPRuleMode(mb) {
		// In rule mode fake-ip-filter is a first-match rule list.
		sets = map[string]bool{}
		for k, v := range setPaths {
			sets[k] = v
		}
		delete(sets, "dns.fake-ip-filter")
	}
	diffSettingsIn("", settingsA, settingsB, &d.Settings, sets)
	d.Proxies = diffNamedList(ma["proxies"], mb["proxies"], true)
	if d.Proxies.Reordered && (includesAllProxies(ma) || includesAllProxies(mb)) {
		d.Proxies.OrderMatters = true
	}
	d.ProxyGroups = diffNamedList(ma["proxy-groups"], mb["proxy-groups"], false)
	d.ProxyProviders = diffNamedMap(ma["proxy-providers"], mb["proxy-providers"])
	d.RuleProviders = diffNamedMap(ma["rule-providers"], mb["rule-providers"])
	d.Rules = DiffRules(stringList(ma["rules"]), stringList(mb["rules"]))
	d.SubRules = diffSubRules(ma["sub-rules"], mb["sub-rules"])
	return d, nil
}

// includesAllProxies reports whether a proxy group takes every proxy of the
// proxies list (include-all or include-all-proxies), in list order.
// include-all-providers alone does not: it adds provider proxies only.
func includesAllProxies(m map[string]any) bool {
	groups, _ := m["proxy-groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		for _, k := range []string{"include-all", "include-all-proxies"} {
			switch v := gm[k].(type) {
			case bool:
				if v {
					return true
				}
			case string: // mihomo decodes weakly typed
				if strings.EqualFold(strings.TrimSpace(v), "true") {
					return true
				}
			}
		}
	}
	return false
}

func decodeTop(data []byte) (map[string]any, error) {
	root, err := Parse(data) // expands anchors and merge keys
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := root.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func stringList(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))
	for _, x := range l {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

// fakeIPRuleMode reports whether dns.fake-ip-filter-mode is "rule".
func fakeIPRuleMode(top map[string]any) bool {
	dns, _ := top["dns"].(map[string]any)
	m, _ := dns["fake-ip-filter-mode"].(string)
	return strings.EqualFold(strings.TrimSpace(m), "rule")
}

func diffSettings(path string, a, b any, out *[]SettingChange) {
	diffSettingsIn(path, a, b, out, setPaths)
}

func diffSettingsIn(path string, a, b any, out *[]SettingChange, sets map[string]bool) {
	ma, aok := a.(map[string]any)
	mb, bok := b.(map[string]any)
	if aok && bok {
		for _, k := range unionKeys(ma, mb) {
			p := k
			if path != "" {
				p = path + "." + k
			}
			va, ina := ma[k]
			vb, inb := mb[k]
			switch {
			case !ina:
				*out = append(*out, SettingChange{Path: p, Kind: "added", B: redactValue(k, vb)})
			case !inb:
				*out = append(*out, SettingChange{Path: p, Kind: "removed", A: redactValue(k, va)})
			default:
				diffSettingsIn(p, va, vb, out, sets)
			}
		}
		return
	}
	key := lastKey(path)
	la, aok := a.([]any)
	lb, bok := b.([]any)
	if aok && bok && sets[path] {
		sa, sb := stringSet(la), stringSet(lb)
		var onlyA, onlyB []string
		for _, x := range sortedKeys(sa) {
			if !sb[x] {
				onlyA = append(onlyA, x)
			}
		}
		for _, x := range sortedKeys(sb) {
			if !sa[x] {
				onlyB = append(onlyB, x)
			}
		}
		for _, x := range onlyA {
			*out = append(*out, SettingChange{Path: path, Kind: "removed", A: redactValue(key, x)})
		}
		for _, x := range onlyB {
			*out = append(*out, SettingChange{Path: path, Kind: "added", B: redactValue(key, x)})
		}
		if len(onlyA) == 0 && len(onlyB) == 0 && !reflect.DeepEqual(la, lb) {
			*out = append(*out, SettingChange{Path: path, Kind: "reordered"})
		}
		return
	}
	if caseInsensitive[path] {
		if sa, ok := a.(string); ok {
			if sb, ok := b.(string); ok && strings.EqualFold(sa, sb) {
				return
			}
		}
	}
	if !reflect.DeepEqual(a, b) {
		*out = append(*out, SettingChange{Path: path, Kind: "changed", A: redactValue(key, a), B: redactValue(key, b)})
	}
}

func unionKeys(a, b map[string]any) []string {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return sortedKeys(keys)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func stringSet(l []any) map[string]bool {
	m := map[string]bool{}
	for _, x := range l {
		m[compact(x)] = true
	}
	return m
}

// compact renders a value on one line.
func compact(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// ContentHash is a short, stable hash of a value (for proxies and providers:
// lets two configs be compared without printing credentials).
func ContentHash(v any) string {
	b, _ := json.Marshal(v) // map keys are sorted by encoding/json
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:6])
}

func named(v any) (names []string, items map[string]any) {
	items = map[string]any{}
	l, _ := v.([]any)
	for _, it := range l {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		n := fmt.Sprint(m["name"])
		names = append(names, n)
		items[n] = m
	}
	return names, items
}

func diffNamedList(a, b any, secret bool) NamedDiff {
	na, ia := named(a)
	nb, ib := named(b)
	d := NamedDiff{CountA: len(na), CountB: len(nb)}
	if secret {
		d.Hashes = map[string]string{}
	}
	for _, n := range na {
		vb, ok := ib[n]
		if !ok {
			d.OnlyA = append(d.OnlyA, n)
			if secret {
				d.Hashes[n] = ContentHash(ia[n])
			}
			continue
		}
		if secret {
			d.Hashes[n] = ContentHash(vb)
		}
		if !reflect.DeepEqual(ia[n], vb) {
			d.Changed = append(d.Changed, n)
			if !secret {
				ga, _ := ia[n].(map[string]any)
				gb, _ := vb.(map[string]any)
				var ch []SettingChange
				diffSettings("", ga, gb, &ch)
				for _, c := range ch {
					c.Path = n + "." + c.Path
					d.Details = append(d.Details, c)
				}
			}
		}
	}
	for _, n := range nb {
		if _, ok := ia[n]; !ok {
			d.OnlyB = append(d.OnlyB, n)
			if secret {
				d.Hashes[n] = ContentHash(ib[n])
			}
		}
	}
	if d.Same() && !equalStrings(na, nb) {
		d.Reordered = true
	}
	return d
}

// diffNamedMap compares provider maps by name and content hash only.
func diffNamedMap(a, b any) NamedDiff {
	ma, _ := a.(map[string]any)
	mb, _ := b.(map[string]any)
	d := NamedDiff{CountA: len(ma), CountB: len(mb), Hashes: map[string]string{}}
	for _, k := range unionKeys(ma, mb) {
		va, ina := ma[k]
		vb, inb := mb[k]
		switch {
		case !inb:
			d.OnlyA = append(d.OnlyA, k)
			d.Hashes[k] = ContentHash(va)
		case !ina:
			d.OnlyB = append(d.OnlyB, k)
			d.Hashes[k] = ContentHash(vb)
		default:
			d.Hashes[k] = ContentHash(vb)
			if !reflect.DeepEqual(va, vb) {
				d.Changed = append(d.Changed, k)
			}
		}
	}
	if len(d.Hashes) == 0 {
		d.Hashes = nil
	}
	return d
}

func diffSubRules(a, b any) []string {
	ma, _ := a.(map[string]any)
	mb, _ := b.(map[string]any)
	var out []string
	for _, k := range unionKeys(ma, mb) {
		ra, rb := stringList(ma[k]), stringList(mb[k])
		_, ina := ma[k]
		_, inb := mb[k]
		if !ina || !inb || !DiffRules(ra, rb).Equivalent {
			out = append(out, k)
		}
	}
	return out
}

// Text renders the diff for people, in full. Proxy and provider contents
// are never shown; at most limit rule edits and dead rules are listed
// (0 = 40).
func (d *SemDiff) Text(limit int) string {
	return d.Render(TextOptions{Limit: limit, Verbose: true})
}

// TextOptions shape Render.
type TextOptions struct {
	// Limit is how many rule edits, settings and dead rules are listed at
	// most per list (0 = 40).
	Limit int
	// Verbose lists the shadowed and redundant rules of both sides in
	// full. Without it, those that are the same in A and B (a
	// subscription's own dead rules, which the change does not touch) are
	// folded into a count and the first Examples of them.
	Verbose bool
	// Examples is how many folded dead rules are shown (0 = 5).
	Examples int
	// More says how to see everything, e.g. "--verbose lists them all"
	// (shown after a folded list; "" = nothing).
	More string
}

// Headline is the diff in one line: the counts that changed.
func (d *SemDiff) Headline() string {
	var parts []string
	if n := len(d.EffectiveSettings()); n > 0 {
		parts = append(parts, plural(n, "setting change", "setting changes"))
	}
	for _, x := range []struct {
		name string
		n    NamedDiff
	}{{"proxies", d.Proxies}, {"proxy-providers", d.ProxyProviders}, {"proxy-groups", d.ProxyGroups}, {"rule-providers", d.RuleProviders}} {
		if !x.n.Same() {
			parts = append(parts, fmt.Sprintf("%s: +%d -%d ~%d", x.name, len(x.n.OnlyB), len(x.n.OnlyA), len(x.n.Changed)))
		}
	}
	switch r := d.Rules; {
	case !r.Equivalent:
		parts = append(parts, plural(len(r.Edits), "rule edit", "rule edits"))
	case r.Reordered:
		parts = append(parts, "rules reordered within same-target blocks")
	}
	if len(d.SubRules) > 0 {
		parts = append(parts, plural(len(d.SubRules), "sub-rule set differs", "sub-rule sets differ"))
	}
	if len(parts) == 0 {
		return "no change in behavior"
	}
	return strings.Join(parts, "; ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// splitDead separates the dead rules of A and B into those that are the
// same on both sides (by line and kind, in A's order) and those only in A
// or only in B.
func splitDead(a, b []DeadRule) (common, onlyA, onlyB []DeadRule) {
	key := func(x DeadRule) string {
		if x.Redundant {
			return "r\x00" + x.Rule
		}
		return "s\x00" + x.Rule
	}
	inB := map[string]int{}
	for _, x := range b {
		inB[key(x)]++
	}
	inA := map[string]int{}
	for _, x := range a {
		if k := key(x); inB[k] > 0 {
			inB[k]--
			inA[k]++
			common = append(common, x)
		} else {
			onlyA = append(onlyA, x)
		}
	}
	for _, x := range b {
		if k := key(x); inA[k] > 0 {
			inA[k]--
		} else {
			onlyB = append(onlyB, x)
		}
	}
	return common, onlyA, onlyB
}

// Render renders the diff for people (see TextOptions). It starts with
// the headline and the setting changes; long lists come last.
func (d *SemDiff) Render(o TextOptions) string {
	limit := o.Limit
	if limit <= 0 {
		limit = 40
	}
	examples := o.Examples
	if examples <= 0 {
		examples = 5
	}
	var w strings.Builder
	p := func(f string, a ...any) { fmt.Fprintf(&w, f, a...) }
	p("summary: %s\n", d.Headline())
	if d.Equivalent() {
		p("equivalent: yes\n")
	} else {
		p("equivalent: no\n")
	}
	eff := d.EffectiveSettings()
	p("settings: %d effective change(s)\n", len(eff))
	for i, c := range d.Settings {
		if i == limit && !o.Verbose {
			p("  … %d more setting change(s)\n", len(d.Settings)-limit)
			break
		}
		p("  %s\n", c)
	}
	named := func(title string, n NamedDiff, showDetails bool) {
		state := "same"
		if n.Reordered && n.OrderMatters {
			state = "same items, different order (order matters: include-all groups)"
		} else if !n.Same() {
			state = "differ"
		} else if n.Reordered {
			state = "same items, different order"
		}
		p("%s: A=%d B=%d, %s\n", title, n.CountA, n.CountB, state)
		for _, x := range n.OnlyA {
			p("  only in A: %q\n", x)
		}
		for _, x := range n.OnlyB {
			p("  only in B: %q\n", x)
		}
		for _, x := range n.Changed {
			p("  changed: %q\n", x)
		}
		if showDetails {
			for _, c := range n.Details {
				p("    %s\n", c)
			}
		}
	}
	named("proxies", d.Proxies, false)
	if d.ProxyProviders.CountA+d.ProxyProviders.CountB > 0 {
		named("proxy-providers", d.ProxyProviders, false)
	}
	named("proxy-groups", d.ProxyGroups, true)
	if d.RuleProviders.CountA+d.RuleProviders.CountB > 0 {
		named("rule-providers", d.RuleProviders, false)
	}
	for _, s := range d.SubRules {
		p("sub-rules: %q differs\n", s)
	}
	r := d.Rules
	state := "differ"
	switch {
	case r.Identical:
		state = "identical"
	case r.Equivalent && r.Reordered:
		state = "equivalent (same-target blocks reordered)"
	case r.Equivalent:
		state = "equivalent"
	}
	p("rules: A=%d B=%d, %s\n", r.CountA, r.CountB, state)
	p("  live rules: A=%d B=%d; exact duplicates: A=%d B=%d; shadowed or redundant: A=%d B=%d\n",
		r.LiveA, r.LiveB, r.DupA, r.DupB, len(r.DeadA), len(r.DeadB))
	dead := func(side string, l []DeadRule, n int) {
		for i, x := range l {
			if i == n {
				p("  … %d more shadowed or redundant in %s\n", len(l)-n, side)
				break
			}
			how := "shadowed"
			if x.Redundant {
				how = "redundant"
			}
			p("  %s in %s: #%d %s (covered by #%d)\n", how, side, x.Index+1, x.Rule, x.CoveredBy+1)
		}
	}
	if o.Verbose {
		dead("A", r.DeadA, limit)
		dead("B", r.DeadB, limit)
	} else {
		common, onlyA, onlyB := splitDead(r.DeadA, r.DeadB)
		dead("A only", onlyA, limit)
		dead("B only", onlyB, limit)
		if len(common) > 0 {
			p("  shadowed or redundant in both A and B: %d (unchanged)", len(common))
			if len(common) > examples {
				p(", the first %d", examples)
			}
			p(":\n")
			for i, x := range common {
				if i == examples {
					break
				}
				p("    #%d %s (covered by #%d)\n", x.Index+1, x.Rule, x.CoveredBy+1)
			}
			if len(common) > examples && o.More != "" {
				p("    (%s)\n", o.More)
			}
		}
	}
	for i, e := range r.Edits {
		if i == limit {
			p("  … %d more edits\n", len(r.Edits)-limit)
			break
		}
		p("  %s\n", e)
	}
	return w.String()
}

// SemanticDiffNodes is SemanticDiff on parsed roots.
func SemanticDiffNodes(a, b *yaml.Node) (*SemDiff, error) {
	ea, err := Encode(a)
	if err != nil {
		return nil, err
	}
	eb, err := Encode(b)
	if err != nil {
		return nil, err
	}
	return SemanticDiff(ea, eb)
}
