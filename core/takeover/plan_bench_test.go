package takeover

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/internal/rulegen"
)

// synthLive is liveHeader + the test proxies and groups + a large synthetic
// rule list (the shape of a big personal config), with hand-written head
// rules in front.
func synthLive() (live, sub string) {
	var w strings.Builder
	w.WriteString("rules:\n")
	for _, r := range rulegen.Shaped(3, rulegen.TypicalBlocks) {
		w.WriteString("  - " + r + "\n")
	}
	tail := w.String()
	head := strings.Replace(liveRules, "  - DOMAIN-SUFFIX,example.cn,DIRECT\n  - GEOIP,CN,DIRECT\n  - GEOIP,CN,DIRECT\n  - MATCH,PROXY\n", "", 1)
	body := strings.Split(subBody, "rules:\n")[0]
	live = liveHeader + body + head + strings.TrimPrefix(tail, "rules:\n")
	sub = "port: 7890\nallow-lan: true\nmode: Rule\n" + body + tail
	return live, sub
}

func BenchmarkBuildPlanTypical(b *testing.B) {
	live, sub := synthLive()
	dir := b.TempDir()
	for _, d := range []string{"conf", "backup", "bin"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			b.Fatal(err)
		}
	}
	files := map[string]string{"conf/config.yaml": live, "backup/raw-sub.yaml": sub,
		"conf/config.yaml.bak-1": live, "conf/config.yaml.bak-2": live, "conf/geoip.metadb": "x"}
	for p, s := range files {
		if err := os.WriteFile(filepath.Join(dir, p), []byte(s), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := BuildPlan(context.Background(), fakeProbe{dir}, Options{NoValidate: true,
			Now: func() time.Time { return time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC) }})
		if err != nil {
			b.Fatal(err)
		}
		if i == 0 && (p.Base == nil || !strings.HasSuffix(p.Base.Path, "raw-sub.yaml")) {
			b.Fatalf("unexpected base: %+v", p.Base)
		}
	}
}
