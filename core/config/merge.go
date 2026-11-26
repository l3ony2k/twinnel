package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Merge applies an override (patch) mapping onto dst in place, using the
// Clash Party (formerly Mihomo Party) override semantics:
//
//   - mappings are merged recursively;
//   - "key!" replaces the value of key wholesale (any type);
//   - sequences replace by default; "+key" prepends, "key+" appends;
//   - scalars replace;
//   - "<key>" wraps a key that literally contains "+" or "!", e.g.
//     "<+.ts.net>" in dns.nameserver-policy. Markers go outside the brackets:
//     "+<key>", "<key>+", "<key>!".
//
// Differences from Clash Party, all deliberate:
//   - "key!" works for every value type, not only mappings;
//   - markers inside values that are inserted wholesale (new keys, "key!"
//     replacements) are resolved too, so the output never contains marker keys;
//   - "+key"/"key+" with a null value is a no-op instead of creating a literal
//     key;
//   - when a mapping is merged onto a non-mapping, the old value is discarded
//     (Clash Party keeps a list and throws on a scalar);
//   - "<key>" unwraps for every value type (Clash Party keeps the brackets on
//     scalar values and drops the value of a wrapped mapping key);
//   - "+key"/"key+" with a non-list value is an error (Clash Party writes a
//     literal "+key").
//
// TestMergeMatchesClashParty checks all of this against a port of
// Clash Party's merge.ts.
//
// The patch is not modified; values are deep-copied into dst.
func Merge(dst, patch *yaml.Node) error {
	if dst == nil || dst.Kind != yaml.MappingNode {
		return fmt.Errorf("merge target must be a mapping")
	}
	if isNull(patch) {
		return nil
	}
	patch = expand(patch)
	if patch.Kind != yaml.MappingNode {
		return fmt.Errorf("override must be a mapping, got %s", kindName(patch.Kind))
	}
	return mergeMap(dst, patch, "")
}

// MergeBytes is Merge on encoded documents.
func MergeBytes(base, patch []byte) ([]byte, error) {
	b, err := Parse(base)
	if err != nil {
		return nil, fmt.Errorf("base: %w", err)
	}
	p, err := Parse(patch)
	if err != nil {
		return nil, fmt.Errorf("override: %w", err)
	}
	if err := Merge(b, p); err != nil {
		return nil, err
	}
	return Encode(b)
}

type op int

const (
	opMerge op = iota
	opReplace
	opPrepend
	opAppend
)

// parseKey splits a patch key into the target key and the operation.
func parseKey(key string) (string, op) {
	switch {
	case strings.HasPrefix(key, "+") && len(key) > 1:
		return trimWrap(key[1:]), opPrepend
	case strings.HasSuffix(key, "+") && len(key) > 1:
		return trimWrap(key[:len(key)-1]), opAppend
	case strings.HasSuffix(key, "!") && len(key) > 1:
		return trimWrap(key[:len(key)-1]), opReplace
	}
	return trimWrap(key), opMerge
}

func trimWrap(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") {
		return s[1 : len(s)-1]
	}
	return s
}

func mergeMap(dst, src *yaml.Node, path string) error {
	for i := 0; i+1 < len(src.Content); i += 2 {
		kn, v := src.Content[i], src.Content[i+1]
		if kn.Kind != yaml.ScalarNode {
			return fmt.Errorf("%s: non-scalar keys are not supported", path)
		}
		name, o := parseKey(kn.Value)
		p := join(path, name)

		switch o {
		case opPrepend, opAppend:
			if isNull(v) {
				continue
			}
			if v.Kind != yaml.SequenceNode {
				return fmt.Errorf("%s: %q needs a list, got %s", p, kn.Value, kindName(v.Kind))
			}
			add, err := clean(v, p)
			if err != nil {
				return err
			}
			seq := newSeq()
			old := lookup(dst, name)
			var oldItems []*yaml.Node
			if old != nil && old.Kind == yaml.SequenceNode {
				oldItems = old.Content
				seq.Style = old.Style
			}
			if o == opPrepend {
				seq.Content = append(append(seq.Content, add.Content...), oldItems...)
			} else {
				seq.Content = append(append(seq.Content, oldItems...), add.Content...)
			}
			setKeepComments(dst, name, seq, old)
		case opReplace:
			c, err := clean(v, p)
			if err != nil {
				return err
			}
			set(dst, name, c)
		case opMerge:
			if v.Kind == yaml.MappingNode {
				old := lookup(dst, name)
				if old == nil || old.Kind != yaml.MappingNode {
					old = newMap()
					old.Style = v.Style
					set(dst, name, old)
				}
				if err := mergeMap(old, v, p); err != nil {
					return err
				}
				continue
			}
			c, err := clean(v, p)
			if err != nil {
				return err
			}
			setKeepComments(dst, name, c, lookup(dst, name))
		}
	}
	return nil
}

// setKeepComments sets dst[name]=v and carries the old value's head/line
// comment over when the new value has none.
func setKeepComments(dst *yaml.Node, name string, v, old *yaml.Node) {
	if old != nil {
		if v.HeadComment == "" {
			v.HeadComment = old.HeadComment
		}
		if v.LineComment == "" && v.Kind != yaml.ScalarNode {
			v.LineComment = old.LineComment
		}
	}
	set(dst, name, v)
}

// clean deep-copies a patch value, resolving markers in nested mappings.
func clean(v *yaml.Node, path string) (*yaml.Node, error) {
	switch v.Kind {
	case yaml.MappingNode:
		m := newMap()
		m.Style, m.HeadComment, m.LineComment, m.FootComment = v.Style, v.HeadComment, v.LineComment, v.FootComment
		if err := mergeMap(m, v, path); err != nil {
			return nil, err
		}
		return m, nil
	default:
		// Items of lists (proxies, groups, rules) are data, not overrides:
		// copy them verbatim, like Clash Party does.
		return deepCopy(v), nil
	}
}

func deepCopy(n *yaml.Node) *yaml.Node {
	c := *n
	if n.Content != nil {
		c.Content = make([]*yaml.Node, len(n.Content))
		for i, ch := range n.Content {
			c.Content[i] = deepCopy(ch)
		}
	}
	return &c
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
