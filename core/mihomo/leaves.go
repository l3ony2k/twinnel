package mihomo

// IsBuiltin reports whether the proxy named name (of type typ, "" when
// unknown) is one of mihomo's built-in outbounds: DIRECT, REJECT,
// REJECT-DROP, PASS, COMPATIBLE (and the dns outbound). They carry no
// remote server, so a delay test of them means nothing: REJECT always
// "times out".
func IsBuiltin(name, typ string) bool {
	switch typ {
	case "Direct", "Reject", "RejectDrop", "Pass", "Compatible", "Dns":
		return true
	case "":
		switch name {
		case "DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE":
			return true
		}
	}
	return false
}

// Leaves returns the nodes reachable from the given groups: their members,
// with nested groups opened (YouTube → PROXY → nodes), each node once, in
// the order first met. Built-ins and names /proxies does not know are left
// out; loops between groups are cut.
func Leaves(all map[string]Proxy, groups ...string) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		p, ok := all[name]
		if !ok {
			return
		}
		if p.IsGroup() {
			for _, m := range p.All {
				walk(m)
			}
			return
		}
		if !IsBuiltin(name, p.Type) {
			out = append(out, name)
		}
	}
	for _, g := range groups {
		walk(g)
	}
	return out
}
