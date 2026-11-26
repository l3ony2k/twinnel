package health

import (
	"fmt"
	"net/netip"
	"strings"
)

// probe describes a connection to evaluate against mihomo rules. Only the
// fields that are set are compared.
type probe struct {
	domain  string
	process string
	ip      netip.Addr
}

func (p probe) String() string {
	switch {
	case p.process != "":
		return "process " + p.process
	case p.domain != "":
		return p.domain
	}
	return p.ip.String()
}

// matchRule reports whether rule r definitely matches p, or might match
// (rule types whose content we cannot see, such as GEOSITE or RULE-SET, and
// conditions on things the probe does not pin down). Unknown types are
// "might match": a false "does not match" could turn a broken setup into an
// OK result.
func matchRule(r MihomoRule, p probe) (match, maybe bool) {
	payload := strings.ToLower(r.Payload)
	switch r.Type {
	case "MATCH":
		return true, false
	case "DOMAIN":
		return p.domain != "" && strings.EqualFold(p.domain, payload), false
	case "DOMAINSUFFIX":
		d := strings.ToLower(p.domain)
		return p.domain != "" && (d == payload || strings.HasSuffix(d, "."+payload)), false
	case "DOMAINKEYWORD":
		return p.domain != "" && strings.Contains(strings.ToLower(p.domain), payload), false
	case "DOMAINREGEX", "DOMAINWILDCARD", "GEOSITE":
		return false, p.domain != ""
	case "PROCESSNAME":
		if p.process != "" {
			return strings.EqualFold(p.process, r.Payload), false
		}
		// Any process may open a connection to a domain or IP.
		return false, true
	case "PROCESSPATH", "PROCESSNAMEREGEX", "PROCESSPATHREGEX", "PROCESSNAMEWILDCARD", "PROCESSPATHWILDCARD":
		// The binary's path is unknown: a path ending in /tailscaled may
		// still not be the running one.
		return false, true
	case "IPCIDR", "IPCIDR6":
		if !p.ip.IsValid() {
			// A domain is resolved for IP rules unless no-resolve (which
			// the runtime API does not report); a process connects to IPs.
			return false, true
		}
		pre, ok := ParsePrefixLoose(strings.Split(r.Payload, ",")[0])
		return ok && pre.Contains(p.ip), false
	case "GEOIP", "IPASN", "IPSUFFIX":
		return false, true
	case "DOMAINSET", "IPSET":
		return false, true
	case "NETWORK", "DSTPORT", "SRCPORT", "INPORT", "INTYPE", "INUSER", "INNAME",
		"SRCIPCIDR", "SRCGEOIP", "SRCIPASN", "SRCIPSUFFIX", "UID", "DSCP":
		// Conditions on the network, ports, source or inbound: the probe
		// leaves them open.
		return false, true
	case "RULESET", "AND", "OR", "NOT", "SUBRULE", "SUBRULES":
		return false, true
	}
	return false, true
}

// evalRules finds the first rule that matches p. shadow lists earlier
// rules that might match and do not go DIRECT.
func evalRules(rules []MihomoRule, p probe) (first *MihomoRule, shadow []MihomoRule) {
	for i := range rules {
		m, maybe := matchRule(rules[i], p)
		if m {
			return &rules[i], shadow
		}
		if maybe && rules[i].Target != "DIRECT" {
			shadow = append(shadow, rules[i])
		}
	}
	return nil, shadow
}

func ruleString(r MihomoRule) string {
	if r.Payload == "" {
		return fmt.Sprintf("#%d %s → %s", r.Index, r.Type, r.Target)
	}
	return fmt.Sprintf("#%d %s,%s → %s", r.Index, r.Type, r.Payload, r.Target)
}
