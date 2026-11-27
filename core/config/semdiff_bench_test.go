package config

import (
	"testing"

	"github.com/l3ony2k/twinnel/core/internal/rulegen"
)

// coexistHead is a synthetic coexist-style prefix.
var coexistHead = []string{
	"PROCESS-NAME,tailscaled,DIRECT",
	"DOMAIN-SUFFIX,ts.net,DIRECT",
	"DOMAIN-SUFFIX,tailscale.com,DIRECT",
	"DOMAIN,alpha,DIRECT",
	"DOMAIN,beta,DIRECT",
	"IP-CIDR,100.64.0.0/10,DIRECT,no-resolve",
	"IP-CIDR,100.100.100.100/32,DIRECT,no-resolve",
	"IP-CIDR6,fd7a:115c:a1e0::/48,DIRECT,no-resolve",
}

func BenchmarkCanonicalRulesTypical(b *testing.B) {
	rules := rulegen.Shaped(1, rulegen.TypicalBlocks)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CanonicalRules(rules)
	}
}

func BenchmarkDiffRulesTypical(b *testing.B) {
	a := rulegen.Shaped(1, rulegen.TypicalBlocks)
	c := append(append([]string(nil), coexistHead...), a...)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DiffRules(a, c)
	}
}

func BenchmarkDiffRulesMixed9000(b *testing.B) {
	a := rulegen.Rules(1, 9000)
	c := append(append([]string(nil), coexistHead...), a...)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DiffRules(a, c)
	}
}
