// Command gen writes health-warn.json, the README shots' health fixture
// with one warning (a tailnet device whose short name has no DIRECT rule),
// and prints every check's result for both variants:
//
//	go run ./cmd/app/testdata/readme/gen cmd/app/testdata/readme/health-warn.json
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/health/fixture"
)

func main() {
	for _, variant := range []string{"ok", "warn"} {
		m := fixture.Synthetic()
		if variant == "warn" {
			// A device joined the tailnet after the last apply: its short
			// name has no DIRECT rule yet.
			drop := func(rs []health.MihomoRule) []health.MihomoRule {
				var out []health.MihomoRule
				for _, r := range rs {
					if r.Payload != "raspberry" {
						out = append(out, r)
					}
				}
				return out
			}
			m.Runtime.Rules = drop(m.Runtime.Rules)
			m.Config.Rules = drop(m.Config.Rules)
		}
		env := &fixture.Env{M: m, Prefs: health.DefaultSettings()}
		for _, c := range health.DefaultRegistry().Checks() {
			r := c.Run(context.Background(), env)
			fmt.Println(variant, c.ID(), r.Status, r.Summary)
		}
		if variant == "warn" {
			b, _ := json.MarshalIndent(m, "", " ")
			os.WriteFile(os.Args[1], b, 0o644)
		}
	}
}
