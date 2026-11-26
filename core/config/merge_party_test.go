package config

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// partyMerge is a port of Clash Party's deepMerge(target, other, true)
// (src/main/utils/merge.ts, branch smart_core, read 2026-11-26) on decoded
// YAML. Kept literal, bugs included, so the differential test below shows
// exactly where duet deliberately differs.
func partyMerge(target, other map[string]any) map[string]any {
	trimWrap := func(s string) string {
		if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") {
			return s[1 : len(s)-1]
		}
		return s
	}
	truthy := func(v any) bool {
		switch x := v.(type) {
		case nil:
			return false
		case bool:
			return x
		case string:
			return x != ""
		case int:
			return x != 0
		case float64:
			return x != 0
		}
		return true
	}
	// JS iterates keys in insertion order; the table below never depends on
	// the order of two keys of one mapping, so map order is fine.
	for key, ov := range other {
		switch v := ov.(type) {
		case map[string]any:
			if strings.HasSuffix(key, "!") {
				target[trimWrap(key[:len(key)-1])] = v
			} else {
				k := trimWrap(key)
				if !truthy(target[k]) {
					target[k] = map[string]any{}
				}
				t, ok := target[k].(map[string]any)
				if !ok {
					// An array keeps its items (JS sets properties on it); a
					// truthy scalar throws (strict mode). Either way the old
					// value stays.
					continue
				}
				// merge.ts recurses with other[k], not other[key]: for a
				// wrapped key "<k>" that is undefined, so nothing is merged.
				sub, _ := other[k].(map[string]any)
				partyMerge(t, sub)
			}
		case []any:
			switch {
			case strings.HasPrefix(key, "+"):
				k := trimWrap(key[1:])
				old, _ := target[k].([]any)
				target[k] = append(append([]any{}, v...), old...)
			case strings.HasSuffix(key, "+"):
				k := trimWrap(key[:len(key)-1])
				old, _ := target[k].([]any)
				target[k] = append(append([]any{}, old...), v...)
			default:
				target[trimWrap(key)] = v
			}
		default:
			target[key] = v
		}
	}
	return target
}

func decodeAny(t *testing.T, s string) map[string]any {
	t.Helper()
	n, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := n.Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// TestMergeMatchesClashParty runs duet's Merge and the Clash Party algorithm
// on tricky inputs. same=true cases must agree; same=false cases are the
// documented deliberate differences (see Merge's doc comment) and must still
// differ, so a change in either side is noticed.
func TestMergeMatchesClashParty(t *testing.T) {
	cases := []struct {
		name, base, patch string
		same              bool
	}{
		{"scalar replace", "a: 1\nb: x\n", "a: 2\n", true},
		{"deep map", "dns: {enable: false, listen: ':53'}\n", "dns: {enable: true, ipv6: false}\n", true},
		{"new map with nested prepend", "a: 1\n", "dns:\n  '+nameserver': [1.1.1.1]\n  sub: {'x+': [1]}\n", true},
		{"prepend and append", "rules: [b]\n", "'+rules': [a]\n'rules+': [c]\n", true},
		{"prepend onto missing", "a: 1\n", "'+rules': [x]\n", true},
		{"list replace", "rules: [a, b]\n", "rules: [c]\n", true},
		{"wrapped list key", "dns: {nameserver-policy: {}}\n", "dns:\n  nameserver-policy:\n    '<+.ts.net>': [100.100.100.100]\n", true},
		{"prepend into wrapped key", "p: {'+.a': [x]}\n", "p:\n  '+<+.a>': [y]\n", true},
		{"unwrapped plus key is a prepend", "p: {}\n", "p:\n  '+.ts.net': [1]\n", true},
		{"null scalar", "a: 1\nb: 2\n", "a: null\n", true},
		{"arrays of maps copied verbatim", "proxies: []\n", "proxies:\n  - {name: x, 'y+': [1]}\n", true},
		{"anchors and merge keys", "a: 1\n", "base: &b {x: 1, y: 2}\nc:\n  <<: *b\n  y: 3\n", true},
		{"bang map", "dns: {enable: true, listen: ':53'}\n", "'dns!': {enable: false}\n", true},

		// Deliberate differences.
		{"bang on list (party keeps literal key)", "rules: [a]\n", "'rules!': [b]\n", false},
		{"bang on scalar (party keeps literal key)", "mode: rule\n", "'mode!': global\n", false},
		{"markers inside a bang map", "dns: {}\n", "'dns!':\n  '+nameserver': [1.1.1.1]\n", false},
		{"null prepend (party sets literal key)", "rules: [a]\n", "'+rules': null\n", false},
		{"wrapped map key (party drops the value)", "p: {}\n", "p:\n  '<a+>': {x: 1}\n", false},
		{"wrapped scalar key (party keeps brackets)", "p: {}\n", "p:\n  '<a!>': 1\n", false},
		{"map onto scalar (party throws in strict mode)", "dns: x\n", "dns: {enable: true}\n", false},
		{"map onto list (party keeps the list)", "dns: [1]\n", "dns: {enable: true}\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := Parse([]byte(c.base))
			if err != nil {
				t.Fatal(err)
			}
			p, err := Parse([]byte(c.patch))
			if err != nil {
				t.Fatal(err)
			}
			if err := Merge(b, p); err != nil {
				t.Fatal(err)
			}
			var ours map[string]any
			if err := b.Decode(&ours); err != nil {
				t.Fatal(err)
			}
			theirs := partyMerge(decodeAny(t, c.base), decodeAny(t, c.patch))
			eq := reflect.DeepEqual(ours, theirs)
			if eq != c.same {
				ob, _ := yaml.Marshal(ours)
				tb, _ := yaml.Marshal(theirs)
				t.Errorf("same=%v, want %v\nduet:\n%s\nparty:\n%s", eq, c.same, ob, tb)
			}
		})
	}
}
