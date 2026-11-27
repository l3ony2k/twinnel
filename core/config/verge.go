package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// vergeSeqKeys maps Clash Verge Rev "Merge" profile keys to the override
// marker syntax, in the order they are emitted.
var vergeSeqKeys = []struct{ verge, party string }{
	{"prepend-rules", "+rules"},
	{"append-rules", "rules+"},
	{"prepend-proxies", "+proxies"},
	{"append-proxies", "proxies+"},
	{"prepend-proxy-groups", "+proxy-groups"},
	{"append-proxy-groups", "proxy-groups+"},
}

// ImportVergeMerge converts a Clash Verge Rev "Merge" file into an override in
// this package's syntax.
//
// The prepend-/append- keys become "+key"/"key+". Every other key is
// deep-merged by Verge, which is also the default here, so it is copied as is;
// keys that would read as markers here (e.g. "+.ts.net" inside
// nameserver-policy) are wrapped in "<...>" so they stay literal. Plain keys are
// emitted first and the prepend/append keys last, matching Verge, which merges
// first and then prepends/appends.
func ImportVergeMerge(data []byte) ([]byte, error) {
	src, err := Parse(data)
	if err != nil {
		return nil, err
	}
	out := newMap()
	out.HeadComment = src.HeadComment
	seq := map[string]*yaml.Node{}
	for i := 0; i+1 < len(src.Content); i += 2 {
		k, v := src.Content[i], src.Content[i+1]
		if isVergeSeqKey(k.Value) {
			if !isNull(v) && v.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("line %d: %s: needs a list, got %s", k.Line, k.Value, kindName(v.Kind))
			}
			seq[k.Value] = v
			continue
		}
		nk := deepCopy(k)
		nk.Value = escapeKey(k.Value)
		out.Content = append(out.Content, nk, escapeKeys(v))
	}
	for _, s := range vergeSeqKeys {
		v, ok := seq[s.verge]
		if !ok || isNull(v) {
			continue
		}
		out.Content = append(out.Content, newStr(s.party), deepCopy(v))
	}
	return Encode(out)
}

func isVergeSeqKey(k string) bool {
	for _, s := range vergeSeqKeys {
		if s.verge == k {
			return true
		}
	}
	return false
}

// escapeKey wraps a literal key that the merge would read as a marker.
func escapeKey(k string) string {
	if len(k) > 1 && (strings.HasPrefix(k, "+") || strings.HasSuffix(k, "+") || strings.HasSuffix(k, "!") ||
		(strings.HasPrefix(k, "<") && strings.HasSuffix(k, ">"))) {
		return "<" + k + ">"
	}
	return k
}

// escapeKeys deep-copies v, escaping keys of nested mappings. List items are
// data and are copied verbatim.
func escapeKeys(v *yaml.Node) *yaml.Node {
	if v.Kind != yaml.MappingNode {
		return deepCopy(v)
	}
	c := *v
	c.Content = make([]*yaml.Node, len(v.Content))
	for i := 0; i+1 < len(v.Content); i += 2 {
		k := deepCopy(v.Content[i])
		k.Value = escapeKey(k.Value)
		c.Content[i] = k
		c.Content[i+1] = escapeKeys(v.Content[i+1])
	}
	return &c
}
