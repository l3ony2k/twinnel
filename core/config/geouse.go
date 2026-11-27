package config

import (
	"crypto/md5"
	"encoding/hex"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Geo database names, as kernel.GeoItems and the geo.* RPCs call them.
const (
	GeoIP   = "geoip"   // GeoIP.dat
	GeoSite = "geosite" // GeoSite.dat
	GeoMMDB = "mmdb"    // Country.mmdb or geoip.metadb
	GeoASN  = "asn"     // ASN.mmdb
)

// GeoUse is which geo databases a mihomo config makes mihomo load.
type GeoUse map[string]bool

// geoRuleRE finds the rule types that load a geo database, also inside
// logical rules (AND,((GEOIP,CN),(NETWORK,UDP)),REJECT).
var geoRuleRE = regexp.MustCompile(`(?i)(?:^|[\s(,])(SRC-GEOIP|GEOIP|GEOSITE|SRC-IP-ASN|IP-ASN)\s*,`)

// geoRefRE finds geosite:/geoip: references (DNS nameserver-policy keys,
// fake-ip-filter entries and the like).
var geoRefRE = regexp.MustCompile(`(?i)(?:^|[^a-z0-9-])(geosite|geoip):`)

// GeoNeeds reads which geo databases cfg makes mihomo load, the way
// mihomo does: GEOSITE rules and geosite: references need GeoSite.dat;
// GEOIP rules, geoip: references and the DNS fallback filter need
// GeoIP.dat with geodata-mode, else the MMDB; IP-ASN rules need ASN.mmdb.
// Rule providers count too: inline payloads, and classical rule sets on
// disk when readFile (a path relative to mihomo's home dir, as the config
// names it) can read them. A config that loads none needs none, and
// mihomo downloads one itself when a rule needs it and it is absent.
func GeoNeeds(cfg []byte, readFile func(rel string) ([]byte, error)) (GeoUse, error) {
	root, err := Parse(cfg)
	if err != nil {
		return nil, err
	}
	u := GeoUse{}
	datMode := false
	if n := lookup(root, "geodata-mode"); n != nil && n.Kind == yaml.ScalarNode {
		datMode = strings.EqualFold(n.Value, "true")
	}
	ip := func() {
		if datMode {
			u[GeoIP] = true
		} else {
			u[GeoMMDB] = true
		}
	}
	rule := func(s string) {
		for _, m := range geoRuleRE.FindAllStringSubmatch(s, -1) {
			switch strings.ToUpper(m[1]) {
			case "GEOSITE":
				u[GeoSite] = true
			case "GEOIP", "SRC-GEOIP":
				ip()
			case "IP-ASN", "SRC-IP-ASN":
				u[GeoASN] = true
			}
		}
	}
	ref := func(s string) {
		for _, m := range geoRefRE.FindAllStringSubmatch(s, -1) {
			if strings.EqualFold(m[1], "geosite") {
				u[GeoSite] = true
			} else {
				ip()
			}
		}
	}
	scalars(lookup(root, "rules"), rule)
	scalars(lookup(root, "sub-rules"), rule)
	if p := lookup(root, "rule-providers"); p != nil && p.Kind == yaml.MappingNode {
		for i := 1; i < len(p.Content); i += 2 {
			providerGeo(p.Content[i], readFile, rule)
		}
	}
	dns := lookup(root, "dns")
	scalars(dns, ref)
	if ff := lookup(dns, "fallback-filter"); ff != nil {
		if g := lookup(ff, "geosite"); g != nil && g.Kind == yaml.SequenceNode && len(g.Content) > 0 {
			u[GeoSite] = true
		}
	}
	if fb := lookup(dns, "fallback"); fb != nil && fb.Kind == yaml.SequenceNode && len(fb.Content) > 0 {
		// The fallback filter's geoip is on unless the config turns it off.
		if g := lookupPath(dns, "fallback-filter", "geoip"); g == nil || !strings.EqualFold(g.Value, "false") {
			ip()
		}
	}
	return u, nil
}

// providerGeo feeds a rule provider's rules to rule: its inline payload,
// or a classical rule set's file on disk (its path, else where mihomo
// keeps a remote one: rules/<md5 of the URL>).
func providerGeo(p *yaml.Node, readFile func(string) ([]byte, error), rule func(string)) {
	if p == nil || p.Kind != yaml.MappingNode {
		return
	}
	scalars(lookup(p, "payload"), rule)
	if v := lookup(p, "behavior"); v == nil || !strings.EqualFold(v.Value, "classical") || readFile == nil {
		return
	}
	if v := lookup(p, "format"); v != nil && strings.EqualFold(v.Value, "mrs") {
		return
	}
	rel := ""
	if v := lookup(p, "path"); v != nil && v.Kind == yaml.ScalarNode {
		rel = v.Value
	} else if v := lookup(p, "url"); v != nil && v.Kind == yaml.ScalarNode && v.Value != "" {
		sum := md5.Sum([]byte(v.Value))
		rel = path.Join("rules", hex.EncodeToString(sum[:]))
	}
	if rel == "" {
		return
	}
	b, err := readFile(rel)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		rule(line)
	}
}

// scalars calls fn with every scalar under n: values and mapping keys.
func scalars(n *yaml.Node, fn func(string)) {
	if n == nil {
		return
	}
	if n.Kind == yaml.ScalarNode {
		fn(n.Value)
		return
	}
	for _, c := range n.Content {
		scalars(c, fn)
	}
}
