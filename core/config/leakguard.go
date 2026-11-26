package config

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LeakGuard returns a transform that keeps the named proxy groups from ever
// sending traffic out of the real IP: DIRECT and proxies of "type: direct"
// are removed from their member list, a group left with no members is set
// to REJECT, and "empty-fallback" is set to REJECT. Without that, mihomo
// falls back to COMPATIBLE (a direct dialer) whenever a group has no members
// at run time: a provider that failed to load or is empty, or filters that
// match nothing. Groups that take members from providers or include-all also
// get "Direct" in exclude-type, so direct-type proxies brought in that way
// are dropped.
//
// It warns (does not fail) when a named group does not exist, and when a
// guarded group still reaches DIRECT through a nested group, since changing
// shared nested groups could break other traffic.
func LeakGuard(groups []string) Transform {
	names := append([]string(nil), groups...)
	return func(root *yaml.Node) ([]string, error) {
		pg := lookup(root, "proxy-groups")
		if pg != nil && pg.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("proxy-groups is not a list")
		}
		byName := map[string]*yaml.Node{}
		if pg != nil {
			for _, g := range pg.Content {
				if n := lookup(g, "name"); n != nil {
					byName[n.Value] = g
				}
			}
		}
		direct := directNames(root)
		var warns []string
		for _, name := range names {
			g := byName[name]
			if g == nil {
				warns = append(warns, fmt.Sprintf("proxy group %q not found", name))
				continue
			}
			members := lookup(g, "proxies")
			if members != nil && members.Kind == yaml.SequenceNode {
				out := members.Content[:0]
				for _, m := range members.Content {
					if m.Kind == yaml.ScalarNode && direct[m.Value] {
						continue
					}
					out = append(out, m)
				}
				members.Content = out
			}
			if groupEmpty(g) {
				set(g, "proxies", strSeq("REJECT"))
				warns = append(warns, fmt.Sprintf("proxy group %q has no members left; set to REJECT", name))
				continue
			}
			if ef := lookup(g, "empty-fallback"); ef == nil || direct[ef.Value] || ef.Value == "COMPATIBLE" {
				set(g, "empty-fallback", newStr("REJECT"))
			}
			if takesDynamicMembers(g) {
				addExcludeType(g, "Direct")
			}
			if via := reachesDirect(g, byName, direct, map[string]bool{name: true}); len(via) > 0 {
				sort.Strings(via)
				warns = append(warns, fmt.Sprintf("proxy group %q can still reach DIRECT through %v", name, via))
			}
		}
		return warns, nil
	}
}

// LeakGuardLayer wraps LeakGuard in a managed layer.
func LeakGuardLayer(groups []string) Layer {
	return Layer{
		ID: "leak-guard", Name: "Leak guard", Kind: KindManaged,
		Stage: StageGuard, Enabled: true, Transform: LeakGuard(groups),
	}
}

// directNames is DIRECT plus the names of proxies of type direct.
func directNames(root *yaml.Node) map[string]bool {
	out := map[string]bool{"DIRECT": true}
	if px := lookup(root, "proxies"); px != nil && px.Kind == yaml.SequenceNode {
		for _, p := range px.Content {
			n, t := lookup(p, "name"), lookup(p, "type")
			if n != nil && t != nil && strings.EqualFold(t.Value, "direct") {
				out[n.Value] = true
			}
		}
	}
	return out
}

// takesDynamicMembers reports whether a group gets members from providers or
// include-all, which can bring in direct-type proxies by type rather than
// by name.
func takesDynamicMembers(g *yaml.Node) bool {
	if u := lookup(g, "use"); u != nil && u.Kind == yaml.SequenceNode && len(u.Content) > 0 {
		return true
	}
	for _, k := range []string{"include-all", "include-all-proxies", "include-all-providers"} {
		if v := lookup(g, k); v != nil && v.Value == "true" {
			return true
		}
	}
	return false
}

// addExcludeType adds t to the group's "|"-separated exclude-type.
func addExcludeType(g *yaml.Node, t string) {
	cur := lookup(g, "exclude-type")
	if cur == nil || cur.Kind != yaml.ScalarNode || strings.TrimSpace(cur.Value) == "" {
		set(g, "exclude-type", newStr(t))
		return
	}
	for _, x := range strings.Split(cur.Value, "|") {
		if strings.EqualFold(strings.TrimSpace(x), t) {
			return
		}
	}
	set(g, "exclude-type", newStr(cur.Value+"|"+t))
}

func groupEmpty(g *yaml.Node) bool {
	if m := lookup(g, "proxies"); m != nil && m.Kind == yaml.SequenceNode && len(m.Content) > 0 {
		return false
	}
	if u := lookup(g, "use"); u != nil && u.Kind == yaml.SequenceNode && len(u.Content) > 0 {
		return false
	}
	for _, k := range []string{"include-all", "include-all-proxies", "include-all-providers"} {
		if v := lookup(g, k); v != nil && v.Value == "true" {
			return false
		}
	}
	return true
}

// reachesDirect returns the nested groups through which g reaches DIRECT.
// stack holds the groups on the current path, to stop at cycles.
func reachesDirect(g *yaml.Node, byName map[string]*yaml.Node, direct, stack map[string]bool) []string {
	members := lookup(g, "proxies")
	if members == nil || members.Kind != yaml.SequenceNode {
		return nil
	}
	var via []string
	for _, m := range members.Content {
		sub := byName[m.Value]
		if sub == nil || stack[m.Value] {
			continue
		}
		stack[m.Value] = true
		if hasDirect(sub, direct) || len(reachesDirect(sub, byName, direct, stack)) > 0 {
			via = append(via, m.Value)
		}
		delete(stack, m.Value)
	}
	return via
}

func hasDirect(g *yaml.Node, direct map[string]bool) bool {
	members := lookup(g, "proxies")
	if members == nil {
		return false
	}
	for _, m := range members.Content {
		if direct[m.Value] {
			return true
		}
	}
	return false
}
