package main

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
)

// What an override does, in a line (its row's meta): how many rules,
// proxies and groups it adds, and which other parts of the config it
// changes (3 条规则 · 改 dns、hosts). Read from its top-level keys, with
// the merge markers of both formats taken off (+rules, rules+, <rules>,
// rules!, prepend-rules, append-proxies, …).

// layerSummary is the counted content of one override.
type layerSummary struct {
	rules, proxies, groups int
	keys                   []string // other top-level keys, in file order
	empty                  bool     // no keys at all (comments only)
	bad                    bool     // not a YAML mapping
}

// summarizeLayer reads an override's content.
func summarizeLayer(content string) layerSummary {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return layerSummary{bad: true}
	}
	if root.Kind == 0 || len(root.Content) == 0 || root.Content[0].Kind == yaml.ScalarNode && root.Content[0].Tag == "!!null" {
		return layerSummary{empty: true}
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode {
		return layerSummary{bad: true}
	}
	var s layerSummary
	seen := map[string]bool{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, val := overrideKey(m.Content[i].Value), m.Content[i+1]
		n := 0
		if val.Kind == yaml.SequenceNode {
			n = len(val.Content)
		}
		switch key {
		case "rules":
			s.rules += n
		case "proxies":
			s.proxies += n
		case "proxy-groups":
			s.groups += n
		default:
			if key != "" && !seen[key] {
				seen[key] = true
				s.keys = append(s.keys, key)
			}
		}
	}
	s.empty = len(m.Content) == 0
	return s
}

// overrideKey is a top-level key without its merge markers: Clash Party's
// +key (prepend), key+ (append), <key> (replace), key! (force), and
// Clash Verge Rev's prepend-/append-/delete- prefixes on rules, proxies
// and proxy-groups.
func overrideKey(k string) string {
	k = strings.TrimSpace(k)
	k = strings.TrimPrefix(k, "<")
	k = strings.TrimSuffix(k, ">")
	k = strings.TrimPrefix(k, "+")
	k = strings.TrimRight(k, "+!")
	for _, p := range []string{"prepend-", "append-", "delete-"} {
		if rest, ok := strings.CutPrefix(k, p); ok && (rest == "rules" || rest == "proxies" || rest == "proxy-groups") {
			return rest
		}
	}
	return k
}

// text is the summary's line in the UI language; "" when it cannot be
// read (the row then says nothing rather than guess).
func (s layerSummary) text() string {
	T := i18n.T
	if s.bad {
		return ""
	}
	var parts []string
	if s.rules > 0 {
		parts = append(parts, i18n.Count(s.rules, T.SumRule, T.SumRules))
	}
	if s.proxies > 0 {
		parts = append(parts, i18n.Count(s.proxies, T.SumProxy, T.SumProxies))
	}
	if s.groups > 0 {
		parts = append(parts, i18n.Count(s.groups, T.SumGroup, T.SumGroups))
	}
	if len(s.keys) > 0 {
		keys := s.keys
		more := ""
		if len(keys) > 3 {
			more = i18n.F(T.SumMoreKeys, len(keys)-3)
			keys = keys[:3]
		}
		parts = append(parts, i18n.F(T.SumChanges, strings.Join(keys, T.ListSep))+more)
	}
	if len(parts) == 0 {
		return T.SumEmpty
	}
	return strings.Join(parts, " · ")
}

// summaryKey is what a cached summary was read from.
func summaryKey(l rpc.Layer) string { return l.Hash }

// loadLayerSummaries reads the stored layers whose content changed since
// their summary was made (layer.get, local reads in the helper). Main
// thread; the summaries arrive on it.
func (a *app) loadLayerSummaries() {
	v := &a.hd.cfg
	if v.sums == nil {
		v.sums = map[string]layerSummary{}
	}
	if v.sumsLoading == nil {
		v.sumsLoading = map[string]bool{}
	}
	hc := a.hc
	for _, l := range a.hd.layers {
		k := summaryKey(l)
		if l.Managed || k == "" {
			continue
		}
		if _, ok := v.sums[k]; ok || v.sumsLoading[k] {
			continue
		}
		v.sumsLoading[k] = true
		id := l.ID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var lc rpc.LayerContent
			err := hc.Call(ctx, rpc.MethodLayerGet, rpc.IDParams{ID: id}, &lc)
			mygo.RunOnMain(func() {
				delete(v.sumsLoading, k)
				if err != nil {
					return // the row keeps its size until the next reload
				}
				v.sums[k] = summarizeLayer(lc.Content)
				a.invalidate()
			})
		}()
	}
}
