// Package config composes mihomo configuration files from a subscription and
// an ordered list of override layers.
//
// All work happens on gopkg.in/yaml.v3 nodes so that key order and comments of
// the subscription and of the override files survive composition.
package config

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse decodes a YAML document and returns its root mapping node with all
// aliases and merge keys ("<<") expanded. An empty document yields an empty
// mapping.
func Parse(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return newMap(), nil
	}
	root := doc.Content[0]
	if root.Kind == yaml.ScalarNode && root.Tag == "!!null" {
		m := newMap()
		m.HeadComment = doc.HeadComment
		return m, nil
	}
	root = expand(root)
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("top level must be a mapping, got %s", kindName(root.Kind))
	}
	if root.HeadComment == "" {
		root.HeadComment = doc.HeadComment
	}
	return root, nil
}

// Encode writes a root mapping node as a YAML document with 2-space indent.
func Encode(root *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func kindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	}
	return "unknown"
}

func newMap() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }
func newSeq() *yaml.Node { return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"} }

func newStr(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	if strings.HasPrefix(s, "+") {
		// Plain "+.ts.net" is valid YAML but easy to misread; quote it like
		// hand-written configs do.
		n.Style = yaml.SingleQuotedStyle
	}
	return n
}

func strSeq(items ...string) *yaml.Node {
	n := newSeq()
	for _, s := range items {
		n.Content = append(n.Content, newStr(s))
	}
	return n
}

// expand returns a deep copy of n with aliases resolved, anchors dropped and
// "<<" merge keys folded into their mapping (explicit keys win). This keeps the
// output valid when a layer replaces the node that defined an anchor.
func expand(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.AliasNode {
		return expand(n.Alias)
	}
	c := *n
	c.Anchor = ""
	c.Alias = nil
	c.Content = nil
	switch n.Kind {
	case yaml.MappingNode:
		var merged []*yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Kind == yaml.ScalarNode && k.Tag == "!!merge" {
				merged = append(merged, v)
				continue
			}
			c.Content = append(c.Content, expand(k), expand(v))
		}
		for _, src := range merged {
			src = expand(src)
			var maps []*yaml.Node
			switch src.Kind {
			case yaml.MappingNode:
				maps = []*yaml.Node{src}
			case yaml.SequenceNode:
				maps = src.Content
			}
			for _, m := range maps {
				if m.Kind != yaml.MappingNode {
					continue
				}
				for i := 0; i+1 < len(m.Content); i += 2 {
					if lookup(&c, m.Content[i].Value) == nil {
						c.Content = append(c.Content, m.Content[i], m.Content[i+1])
					}
				}
			}
		}
	default:
		for _, ch := range n.Content {
			c.Content = append(c.Content, expand(ch))
		}
	}
	return &c
}

// lookup returns the value for key in mapping m, or nil.
func lookup(m *yaml.Node, key string) *yaml.Node {
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

// lookupPath walks nested mappings.
func lookupPath(m *yaml.Node, path ...string) *yaml.Node {
	for _, p := range path {
		m = lookup(m, p)
		if m == nil {
			return nil
		}
	}
	return m
}

// set replaces the value of key in m, keeping its position and the key's
// comments, or appends the pair when key is missing.
func set(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, newStr(key), v)
}

func isNull(n *yaml.Node) bool {
	return n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null")
}
