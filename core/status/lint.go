package status

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/duet/core/i18n"
)

// LintNote is one remark on an override layer, for the layer editor.
type LintNote struct {
	Line  int // 1-based
	Key   string
	Level Level // LevelWarn: very likely a mistake; LevelOK: a reminder of what the key does
	Text  string
}

// LintOverride points at keys an override layer (Clash Party merge syntax,
// see overrides/README.md) treats as merge directives: "+key" prepends,
// "key+" appends, "key!" replaces. A key meant literally (a domain such as
// "+.lan" in hosts) must be wrapped: '<+.lan>'. A directive whose value is
// not a list, or whose name looks like a domain, is flagged as a likely
// mistake; the others get a quiet reminder. A YAML error is one note.
func LintOverride(src string) []LintNote {
	T := i18n.Cur()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return []LintNote{{Level: LevelError, Text: fmt.Sprintf(T.LintYAMLError, err.Error())}}
	}
	var out []LintNote
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		switch n.Kind {
		case yaml.DocumentNode, yaml.SequenceNode:
			for _, c := range n.Content {
				walk(c)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if note, ok := lintKey(k, v); ok {
					out = append(out, note)
				}
				walk(v)
			}
		}
	}
	walk(&doc)
	return out
}

func lintKey(k, v *yaml.Node) (LintNote, bool) {
	T := i18n.Cur()
	key := k.Value
	if k.Kind != yaml.ScalarNode || len(key) < 2 || isWrapped(key) {
		return LintNote{}, false
	}
	var base, what string
	list := false // the directive needs a list value
	switch {
	case strings.HasPrefix(key, "+"):
		base, what, list = key[1:], T.LintPrepend, true
	case strings.HasSuffix(key, "+"):
		base, what, list = key[:len(key)-1], T.LintAppend, true
	case strings.HasSuffix(key, "!"):
		base, what = key[:len(key)-1], T.LintReplace
	default:
		return LintNote{}, false
	}
	if isWrapped(base) {
		return LintNote{}, false // '<a+>+': a literal key with a directive outside
	}
	n := LintNote{Line: k.Line, Key: key, Level: LevelOK, Text: fmt.Sprintf(what, key, base)}
	switch {
	case list && v.Kind != yaml.SequenceNode && v.Tag != "!!null":
		n.Level, n.Text = LevelWarn, fmt.Sprintf(T.LintNeedsList, key, key)
	case strings.ContainsAny(base, ".*:/") || base == "":
		n.Level, n.Text = LevelWarn, fmt.Sprintf(T.LintLooksLiteral, key, key)
	}
	return n, true
}

func isWrapped(s string) bool {
	return len(s) >= 2 && strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">")
}
