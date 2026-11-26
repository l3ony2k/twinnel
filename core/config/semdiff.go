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
	// Details holds per-item field differences; never set for proxies and
	// providers (their fields hold credentials).
	Details []SettingChange `json:"details,omitempty"`
	// Hashes maps name → short content hash of B's items (A's when only in
	// A), for proxies and providers only.
	Hashes map[string]string `json:"hashes,omitempty"`
}

// Same reports whether the items are equal (order aside).
func (n NamedDiff) Same() bool {
	return len(n.OnlyA) == 0 && len(n.OnlyB) == 0 && len(n.Changed) == 0
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
	d.ProxyGroups = diffNamedList(ma["proxy-groups"], mb["proxy-groups"], false)
	d.ProxyProviders = diffNamedMap(ma["proxy-providers"], mb["proxy-providers"])
	d.RuleProviders = diffNamedMap(ma["rule-providers"], mb["rule-providers"])
	d.Rules = DiffRules(stringList(ma["rules"]), stringList(mb["rules"]))
	d.SubRules = diffSubRules(ma["sub-rules"], mb["sub-rules"])
	return d, nil
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

// Text renders the diff for people. Proxy and provider contents are never
// shown; at most limit rule edits and dead rules are listed (0 = 40).
func (d *SemDiff) Text(limit int) string {
	if limit <= 0 {
		limit = 40
	}
	var w strings.Builder
	p := func(f string, a ...any) { fmt.Fprintf(&w, f, a...) }
	if d.Equivalent() {
		p("equivalent: yes\n")
	} else {
		p("equivalent: no\n")
	}
	eff := d.EffectiveSettings()
	p("settings: %d effective change(s)\n", len(eff))
	for _, c := range d.Settings {
		p("  %s\n", c)
	}
	named := func(title string, n NamedDiff, showDetails bool) {
		state := "same"
		if !n.Same() {
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
	dead := func(side string, l []DeadRule) {
		for i, x := range l {
			if i == limit {
				p("  … %d more shadowed in %s\n", len(l)-limit, side)
				break
			}
			how := "shadowed"
			if x.Redundant {
				how = "redundant"
			}
			p("  %s in %s: #%d %s (covered by #%d)\n", how, side, x.Index+1, x.Rule, x.CoveredBy+1)
		}
	}
	dead("A", r.DeadA)
	dead("B", r.DeadB)
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
