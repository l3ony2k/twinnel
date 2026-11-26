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
// (rule types whose content we cannot see, such as GEOSITE or RULE-SET).
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
		return p.process != "" && strings.EqualFold(p.process, r.Payload), false
	case "PROCESSPATH":
		return p.process != "" && strings.HasSuffix(r.Payload, "/"+p.process), false
	case "PROCESSNAMEREGEX", "PROCESSPATHREGEX":
		return false, p.process != ""
	case "IPCIDR", "IPCIDR6":
		if !p.ip.IsValid() {
			return false, false
		}
		pre, ok := ParsePrefixLoose(strings.Split(r.Payload, ",")[0])
		return ok && pre.Contains(p.ip), false
	case "GEOIP", "IPASN":
		return false, p.ip.IsValid()
	case "RULESET", "AND", "OR", "NOT", "SUBRULE":
		return false, true
	}
	return false, false
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
