package config

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

func TestGeoNeeds(t *testing.T) {
	url := "https://rules.example.invalid/classical.yaml"
	sum := md5.Sum([]byte(url))
	files := map[string]string{
		"ruleset/local.yaml":                     "payload:\n  - GEOSITE,example\n",
		"rules/" + hex.EncodeToString(sum[:]):    "IP-ASN,64496\n",
		"ruleset/domains.txt":                    "GEOSITE,never-read\n", // behavior domain: not rules
		"ruleset/classical-with-geo-but-mrs.mrs": "GEOIP,XX\n",
	}
	read := func(rel string) ([]byte, error) {
		if b, ok := files[rel]; ok {
			return []byte(b), nil
		}
		return nil, errors.New("not found")
	}
	cases := []struct {
		name string
		cfg  string
		want GeoUse
	}{
		{"empty", ``, GeoUse{}},
		{"no geo rules", "rules:\n  - DOMAIN-SUFFIX,example.com,PROXY\n  - MATCH,DIRECT\n", GeoUse{}},
		// Windows QA round 1: GEOIP rules, no geodata-mode, no GEOSITE.
		{"geoip uses the mmdb", "rules:\n  - GEOIP,LAN,DIRECT,no-resolve\n  - GEOIP,CN,DIRECT\n  - MATCH,PROXY\n", GeoUse{GeoMMDB: true}},
		{"geodata-mode geoip uses the dat", "geodata-mode: true\nrules:\n  - GEOIP,CN,DIRECT\n", GeoUse{GeoIP: true}},
		{"src-geoip", "rules:\n  - SRC-GEOIP,cn,DIRECT\n", GeoUse{GeoMMDB: true}},
		{"geosite rule", "rules:\n  - GEOSITE,cn,DIRECT\n", GeoUse{GeoSite: true}},
		{"lower case and spaces", "rules:\n  - geosite , cn,DIRECT\n", GeoUse{GeoSite: true}},
		{"asn", "rules:\n  - IP-ASN,13335,PROXY\n  - SRC-IP-ASN,64496,DIRECT\n", GeoUse{GeoASN: true}},
		{"logical rule", "rules:\n  - AND,((GEOIP,CN),(NETWORK,UDP)),REJECT\n", GeoUse{GeoMMDB: true}},
		{"sub-rules", "sub-rules:\n  s1:\n    - GEOSITE,example,DIRECT\n", GeoUse{GeoSite: true}},
		{"a proxy named like a rule is not a rule", "proxies:\n  - {name: \"GEOSITE,x\", type: direct}\nrules:\n  - MATCH,DIRECT\n", GeoUse{}},
		{"dns policy geosite key", "dns:\n  nameserver-policy:\n    \"geosite:cn,private\": [223.5.5.5]\n", GeoUse{GeoSite: true}},
		{"fake-ip-filter geosite", "dns:\n  fake-ip-filter:\n    - geosite:connectivity-check\n", GeoUse{GeoSite: true}},
		{"dns keys named geoip are not references", "dns:\n  fallback-filter:\n    geoip: true\n    geoip-code: CN\n", GeoUse{}},
		{"fallback with the default geoip filter", "dns:\n  fallback: [tls://1.0.0.1]\n", GeoUse{GeoMMDB: true}},
		{"fallback with geoip off", "dns:\n  fallback: [tls://1.0.0.1]\n  fallback-filter:\n    geoip: false\n", GeoUse{}},
		{"fallback filter geosite", "dns:\n  fallback-filter:\n    geoip: false\n    geosite: [gfw]\n", GeoUse{GeoSite: true}},
		{"inline provider", "rule-providers:\n  p:\n    type: inline\n    behavior: classical\n    payload:\n      - GEOIP,CN\n", GeoUse{GeoMMDB: true}},
		{"classical provider file", "rule-providers:\n  p:\n    type: file\n    behavior: classical\n    path: ruleset/local.yaml\n", GeoUse{GeoSite: true}},
		{"remote classical provider in mihomo's cache", "rule-providers:\n  p:\n    type: http\n    behavior: classical\n    url: " + url + "\n", GeoUse{GeoASN: true}},
		{"domain provider is not read", "rule-providers:\n  p:\n    type: file\n    behavior: domain\n    path: ruleset/domains.txt\n", GeoUse{}},
		{"mrs provider is not read", "rule-providers:\n  p:\n    type: file\n    behavior: classical\n    format: mrs\n    path: ruleset/classical-with-geo-but-mrs.mrs\n", GeoUse{}},
		{"unreadable provider", "rule-providers:\n  p:\n    type: file\n    behavior: classical\n    path: ruleset/gone.yaml\n", GeoUse{}},
		{"anchors", "x: &r GEOSITE,cn,DIRECT\nrules:\n  - *r\n", GeoUse{GeoSite: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := GeoNeeds([]byte(c.cfg), read)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
	if _, err := GeoNeeds([]byte("rules: [\n"), nil); err == nil {
		t.Error("a config that does not parse: want an error (callers then count every database)")
	}
	if u, err := GeoNeeds([]byte("rule-providers:\n  p:\n    behavior: classical\n    path: ruleset/local.yaml\n"), nil); err != nil || len(u) != 0 {
		t.Errorf("no reader: %v %v", u, err)
	}
}
