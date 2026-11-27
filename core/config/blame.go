package config

import (
	"regexp"
	"strings"
)

// Culprit is a source of the composed config that holds a name mihomo -t
// complained about: an override (Layer) or the subscription (Layer "").
type Culprit struct {
	Name  string // the name in the error ('missing-node')
	Layer string // the layer's ID; "" for the subscription
	Title string // the layer's name
}

var quotedName = regexp.MustCompile(`'([^']+)'|"([^"]+)"`)

// Blame finds where the names that mihomo -t's errors quote come from
// (`proxy group[0]: 'missing-node' not found`): the enabled user layers
// whose text holds the name, else the subscription when it does. Managed
// layers are generated and left out. A name nobody holds is skipped.
func Blame(errs []string, sub []byte, layers []Layer) []Culprit {
	var out []Culprit
	seen := map[string]bool{}
	for _, e := range errs {
		for _, m := range quotedName.FindAllStringSubmatch(e, -1) {
			name := m[1] + m[2]
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			found := false
			for _, l := range layers {
				if !l.Enabled || l.Kind != KindUser || !holds(string(l.Override), name) {
					continue
				}
				out = append(out, Culprit{Name: name, Layer: l.ID, Title: l.Name})
				found = true
			}
			if !found && holds(string(sub), name) {
				out = append(out, Culprit{Name: name})
			}
		}
	}
	return out
}

// holds reports whether text has name as a whole YAML scalar: not inside
// a longer word.
func holds(text, name string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], name)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(name)
		if (j == 0 || !wordByte(text[j-1])) && (end == len(text) || !wordByte(text[end])) {
			return true
		}
		i = j + 1
	}
}

func wordByte(b byte) bool {
	return b == '_' || b == '-' || b == '.' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
