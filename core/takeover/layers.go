package takeover

import (
	"fmt"
	"net/netip"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/duet/core/config"
)

// DerivedLayer is one override layer derived from the live config.
// Layer name codes (DerivedLayer.NameCode).
const (
	LayerNameBase      = "base"
	LayerNameCoexist   = "tailscale-coexist"
	LayerNameLiveRules = "live-rules"
	LayerNameLeakGuard = "leak-guard" // the helper's managed leak guard layer
)

type DerivedLayer struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Kind    config.Kind `json:"kind"`
	Stage   string      `json:"stage"`
	File    string      `json:"file,omitempty"` // set when written
	Keys    []string    `json:"keys,omitempty"` // top-level keys it sets (base layer)
	Rules   int         `json:"rules,omitempty"`
	Comment string      `json:"comment,omitempty"`
	// NameCode and NameArgs name a generated Name for a UI that words it
	// in its own language (core/i18n TakeoverLayerName): LayerNameBase,
	// LayerNameCoexist, or LayerNameLiveRules (first rule, more count).
	// Empty: Name is the person's own (a comment from the live config).
	NameCode string       `json:"name_code,omitempty"`
	NameArgs []string     `json:"name_args,omitempty"`
	stage    config.Stage `json:"-"`
	data     []byte
	// transform is the layer's transform, if any (the coexist layer adapts
	// fake-ip-filter to fake-ip-filter-mode).
	transform config.Transform
}

// Data is the layer's override YAML (may contain credentials of the live
// config; write it with mode 0600, never print it).
func (l DerivedLayer) Data() []byte { return l.data }

// Layer converts to a compose layer.
func (l DerivedLayer) Layer() config.Layer {
	cl := config.OverrideLayer(l.ID, l.Name, l.Kind, l.stage, l.data)
	cl.Transform = l.transform
	return cl
}

// Replaced is a live item that the managed Tailscale coexist layer replaces.
type Replaced struct {
	Path string `json:"path"` // e.g. "rules", "dns.nameserver-policy"
	Item string `json:"item"` // the rule, key or entry
	Why  string `json:"why"`
}

// Derivation is the result of DeriveLayers.
type Derivation struct {
	Layers   []DerivedLayer `json:"layers"`
	Replaced []Replaced     `json:"replaced_by_coexist,omitempty"`
	Warnings []string       `json:"warnings,omitempty"`
}

// managedSet describes what the Tailscale coexist generator owns.
type managedSet struct {
	filter  map[string]bool
	policy  map[string]bool
	hosts   map[string]bool
	cidrs   []netip.Prefix
	rules   []config.Rule
	domains []string // tailscale domains (suffixes) incl. the MagicDNS suffix
}

// newManagedSet runs the generator for info (and for the "suffix unknown"
// variant, which owns +.ts.net / +.beta.tailscale.net) and collects what it
// writes.
func newManagedSet(info config.TailnetInfo) (*managedSet, error) {
	m := &managedSet{filter: map[string]bool{}, policy: map[string]bool{}, hosts: map[string]bool{}}
	// The policy entries are recognised even when MagicDNS is off now: a
	// live config made while it was on carries them.
	info.MagicDNSOff = false
	variants := []config.TailnetInfo{info}
	if info.MagicDNSSuffix != "" {
		v := info
		v.MagicDNSSuffix = ""
		v.PeerAddrs = nil
		variants = append(variants, v)
	}
	for _, v := range variants {
		b, err := config.TailscaleCoexist(v)
		if err != nil {
			return nil, err
		}
		var o struct {
			Hosts map[string]any `yaml:"hosts"`
			DNS   struct {
				Filter []string       `yaml:"fake-ip-filter+"`
				Policy map[string]any `yaml:"nameserver-policy"`
			} `yaml:"dns"`
			TUN struct {
				Exclude []string `yaml:"route-exclude-address+"`
			} `yaml:"tun"`
			Rules []string `yaml:"+rules"`
		}
		if err := yaml.Unmarshal(b, &o); err != nil {
			return nil, err
		}
		for _, f := range o.DNS.Filter {
			m.filter[f] = true
		}
		m.filter[`DOMAIN-REGEX,^[^.]+$,real-ip`] = true
		for k := range o.DNS.Policy {
			m.policy[unwrap(k)] = true
		}
		for k := range o.Hosts {
			m.hosts[strings.ToLower(k)] = true
		}
		for _, c := range o.TUN.Exclude {
			if p, err := netip.ParsePrefix(c); err == nil {
				m.cidrs = append(m.cidrs, p.Masked())
			}
		}
		for _, r := range o.Rules {
			pr := config.ParseRule(r)
			m.rules = append(m.rules, pr)
			if pr.Type == "DOMAIN-SUFFIX" {
				m.domains = append(m.domains, pr.Payload)
				// The same entry as the layer writes it in
				// fake-ip-filter-mode: rule.
				m.filter["DOMAIN-SUFFIX,"+pr.Payload+",real-ip"] = true
			}
		}
	}
	return m, nil
}

func unwrap(k string) string {
	if strings.HasPrefix(k, "<") && strings.HasSuffix(k, ">") {
		return k[1 : len(k)-1]
	}
	return k
}

// tailscaleDomain reports whether a domain pattern ("+.ts.net", "ts.net",
// "*.x.ts.net") is under a Tailscale domain.
func (m *managedSet) tailscaleDomain(pat string) bool {
	d := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(pat), "+."), "*.")
	for _, s := range m.domains {
		if d == s || strings.HasSuffix(d, "."+s) {
			return true
		}
	}
	return false
}

func (m *managedSet) inTailnet(s string) bool {
	p, err := netip.ParsePrefix(strings.TrimSpace(s))
	if err != nil {
		a, err2 := netip.ParseAddr(strings.TrimSpace(s))
		if err2 != nil {
			return false
		}
		p = netip.PrefixFrom(a, a.BitLen())
	}
	for _, c := range m.cidrs {
		if c.Addr().Is4() == p.Addr().Is4() && c.Bits() <= p.Bits() && c.Contains(p.Addr()) {
			return true
		}
	}
	return false
}

// ruleExplained reports whether a live rule is produced, or made redundant,
// by the coexist layer, and why.
func (m *managedSet) ruleExplained(r config.Rule) (bool, string) {
	for _, g := range m.rules {
		if g.Canonical() == r.Canonical() {
			return true, "generated by the coexist layer"
		}
	}
	if !strings.EqualFold(r.Target, "DIRECT") {
		return false, ""
	}
	for _, g := range m.rules {
		if config.Covers(g, r) {
			return true, "covered by coexist rule " + g.Canonical()
		}
	}
	// A single-label DOMAIN rule for a name that is not a current peer
	// ("router", "nas" on the LAN, a removed peer) is not explained: the
	// coexist layer only writes rules for current peers (matched above), so
	// dropping it would route that name by later rules instead.
	return false, ""
}

// DeriveLayers splits the live config into layers on top of base:
//   - "base": top-level settings that differ from base (Tailscale parts
//     removed, they are the coexist layer's job);
//   - "user-N": runs of rules prepended to the base's rules that the coexist
//     layer does not explain, split at comment lines;
//
// and lists what the managed coexist layer replaces.
func DeriveLayers(liveData, baseData []byte, info config.TailnetInfo) (*Derivation, error) {
	liveRoot, err := config.Parse(liveData)
	if err != nil {
		return nil, fmt.Errorf("live config: %w", err)
	}
	live, err := decodeDoc(liveData)
	if err != nil {
		return nil, fmt.Errorf("live config: %w", err)
	}
	base, err := decodeDoc(baseData)
	if err != nil {
		return nil, fmt.Errorf("base: %w", err)
	}
	ms, err := newManagedSet(info)
	if err != nil {
		return nil, err
	}
	out := &Derivation{}

	// ---- base settings layer ----
	layerRoot := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	layerRoot.HeadComment = "Base settings taken over from the live config (ports, DNS, TUN, controller, UI, ...).\n" +
		"Tailscale entries are left out: the managed tailscale-coexist layer writes them."
	var keys []string
	for i := 0; i+1 < len(liveRoot.Content); i += 2 {
		kn, vn := liveRoot.Content[i], liveRoot.Content[i+1]
		k := kn.Value
		if dataKey[k] {
			continue
		}
		v := deepCopy(vn)
		out.Replaced = append(out.Replaced, ms.strip(k, v)...)
		if k == "hosts" && isEmptyCollection(v) && !isEmptyCollection(vn) {
			continue
		}
		var lv any
		_ = v.Decode(&lv)
		bv, inBase := base.top[k]
		if inBase && reflect.DeepEqual(lv, bv) {
			continue
		}
		key := k
		if inBase && needsReplace(bv, lv) {
			key = k + "!"
		}
		escapeKeys(v)
		nk := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: escapeKey(key, true)}
		nk.HeadComment, nk.LineComment = kn.HeadComment, kn.LineComment
		layerRoot.Content = append(layerRoot.Content, nk, v)
		keys = append(keys, k)
	}
	for k := range base.top {
		if dataKey[k] {
			continue
		}
		if _, ok := live.top[k]; !ok {
			out.Warnings = append(out.Warnings, fmt.Sprintf("base sets %q but the live config does not; the Party syntax cannot delete a key, so it stays", k))
		}
	}
	baseLayer := DerivedLayer{ID: "base", Name: "Base settings", NameCode: LayerNameBase, Kind: config.KindManaged,
		Stage: config.StageBase.String(), stage: config.StageBase, Keys: keys}
	if baseLayer.data, err = config.Encode(layerRoot); err != nil {
		return nil, err
	}
	out.Layers = append(out.Layers, baseLayer)

	// ---- rules ----
	tail := commonTail(live.rules, base.rules)
	if tail < len(base.rules) {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"only the last %d of the base's %d rules are unchanged in the live config; the rest cannot be expressed as prepends",
			tail, len(base.rules)))
	}
	head := live.rules[:len(live.rules)-tail]
	nodes := ruleNodes(liveRoot)
	type seg struct {
		comment string
		rules   []string
	}
	var segs []seg
	var cur *seg
	for i, line := range head {
		r := config.ParseRule(line)
		comment := ""
		if i < len(nodes) {
			comment = cleanComment(nodes[i].HeadComment)
		}
		if ok, why := ms.ruleExplained(r); ok {
			out.Replaced = append(out.Replaced, Replaced{Path: "rules", Item: line, Why: why})
			cur = nil
			continue
		}
		if cur == nil || comment != "" {
			segs = append(segs, seg{comment: comment})
			cur = &segs[len(segs)-1]
		}
		cur.rules = append(cur.rules, line)
	}
	// Each user layer prepends ("+rules"), so a later layer lands in front
	// of an earlier one. Emit the segments last-first to keep live order.
	n := len(segs)
	for i := n - 1; i >= 0; i-- {
		s := segs[i]
		id := fmt.Sprintf("user-%d", n-i)
		name := s.comment
		var nameCode string
		var nameArgs []string
		if name == "" {
			name = "Rules from the live config: " + s.rules[0]
			if len(s.rules) > 1 {
				name += fmt.Sprintf(" and %d more", len(s.rules)-1)
			}
			nameCode, nameArgs = LayerNameLiveRules, []string{shorten(s.rules[0], 60), strconv.Itoa(len(s.rules) - 1)}
		}
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.HeadComment = name
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, r := range s.rules {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: r})
		}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "+rules"}, seq)
		data, err := config.Encode(root)
		if err != nil {
			return nil, err
		}
		out.Layers = append(out.Layers, DerivedLayer{
			ID: id, Name: shorten(name, 80), NameCode: nameCode, NameArgs: nameArgs, Kind: config.KindUser, Stage: config.StageUser.String(),
			stage: config.StageUser, Rules: len(s.rules), Comment: s.comment, data: data,
		})
	}
	return out, nil
}

// strip removes Tailscale-managed entries from the value of top-level key k
// in place and returns what it removed.
func (m *managedSet) strip(k string, v *yaml.Node) []Replaced {
	var out []Replaced
	switch k {
	case "dns":
		if f := mapGet(v, "fake-ip-filter"); f != nil && f.Kind == yaml.SequenceNode {
			f.Content = filterSeq(f.Content, func(s string) bool {
				if m.filter[s] || m.tailscaleDomain(s) {
					out = append(out, Replaced{"dns.fake-ip-filter", s, "tailnet names skip fake-ip via the coexist layer"})
					return false
				}
				return true
			})
		}
		if p := mapGet(v, "nameserver-policy"); p != nil && p.Kind == yaml.MappingNode {
			var keep []*yaml.Node
			for i := 0; i+1 < len(p.Content); i += 2 {
				key := p.Content[i].Value
				if m.policy[key] || m.tailscaleDomain(key) || reverseTailnetZone(key, m) {
					var val any
					_ = p.Content[i+1].Decode(&val)
					out = append(out, Replaced{"dns.nameserver-policy", fmt.Sprintf("%s: %v", key, val), "tailnet names resolve via the coexist layer's MagicDNS resolver"})
					continue
				}
				keep = append(keep, p.Content[i], p.Content[i+1])
			}
			p.Content = keep
			if len(keep) == 0 {
				mapDelete(v, "nameserver-policy")
			}
		}
	case "tun":
		if f := mapGet(v, "route-exclude-address"); f != nil && f.Kind == yaml.SequenceNode {
			f.Content = filterSeq(f.Content, func(s string) bool {
				if m.inTailnet(s) {
					out = append(out, Replaced{"tun.route-exclude-address", s, "tailnet ranges are excluded by the coexist layer"})
					return false
				}
				return true
			})
		}
	case "hosts":
		if v.Kind == yaml.MappingNode {
			var keep []*yaml.Node
			for i := 0; i+1 < len(v.Content); i += 2 {
				key := strings.ToLower(v.Content[i].Value)
				if m.hosts[key] || m.tailscaleDomain(key) {
					out = append(out, Replaced{"hosts", key, "peer hosts are generated by the coexist layer"})
					continue
				}
				keep = append(keep, v.Content[i], v.Content[i+1])
			}
			v.Content = keep
		}
	}
	return out
}

func reverseTailnetZone(key string, m *managedSet) bool {
	k := strings.TrimPrefix(key, "+.")
	return m.policy["+."+k] && (strings.HasSuffix(k, ".in-addr.arpa") || strings.HasSuffix(k, ".ip6.arpa"))
}

func filterSeq(items []*yaml.Node, keep func(string) bool) []*yaml.Node {
	out := items[:0]
	for _, it := range items {
		if it.Kind != yaml.ScalarNode || keep(it.Value) {
			out = append(out, it)
		}
	}
	return out
}

func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func mapDelete(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func isEmptyCollection(n *yaml.Node) bool {
	return (n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode) && len(n.Content) == 0
}

func deepCopy(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = nil
	for _, ch := range n.Content {
		c.Content = append(c.Content, deepCopy(ch))
	}
	return &c
}

// needsReplace reports whether merging live onto base would keep something
// of base that live doesn't have (a map key), so "key!" is required.
func needsReplace(base, live any) bool {
	bm, ok1 := base.(map[string]any)
	lm, ok2 := live.(map[string]any)
	if !ok1 || !ok2 {
		return false
	}
	for k, bv := range bm {
		lv, ok := lm[k]
		if !ok || needsReplace(bv, lv) {
			return true
		}
	}
	return false
}

// escapeKey wraps a key that would read as a merge marker ("+.lan" →
// "<+.lan>"). With marker set, a trailing "!" that the caller added is kept
// outside the brackets.
func escapeKey(k string, marker bool) string {
	suffix := ""
	if marker && strings.HasSuffix(k, "!") {
		k, suffix = k[:len(k)-1], "!"
	}
	if strings.HasPrefix(k, "+") || strings.HasSuffix(k, "+") || strings.HasSuffix(k, "!") ||
		(strings.HasPrefix(k, "<") && strings.HasSuffix(k, ">")) {
		return "<" + k + ">" + suffix
	}
	return k + suffix
}

// escapeKeys wraps marker-like keys in all mappings of n (not inside
// sequences: list items are copied verbatim by Merge).
func escapeKeys(n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		if e := escapeKey(k.Value, false); e != k.Value {
			k.Value = e
			k.Style = yaml.SingleQuotedStyle
		}
		escapeKeys(n.Content[i+1])
	}
}

func cleanComment(c string) string {
	var parts []string
	for _, l := range strings.Split(c, "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#"))
		l = strings.Trim(l, "= ")
		if l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, " / ")
}

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
