package overrides

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/config"
)

// Every template must parse and merge onto an empty config.
func TestTemplatesMerge(t *testing.T) {
	names, err := fs.Glob(Templates, "*.yaml")
	if err != nil || len(names) == 0 {
		t.Fatalf("no templates: %v", err)
	}
	for _, n := range names {
		b, _ := Templates.ReadFile(n)
		out, err := config.MergeBytes(nil, b)
		if err != nil {
			t.Errorf("%s: %v", n, err)
			continue
		}
		if n == "base-tun-fakeip.yaml" {
			if !strings.Contains(string(out), "'+.lan':") {
				t.Errorf("%s: nameserver-policy key not literal:\n%s", n, out)
			}
		}
	}
}
