package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/engine/enginetest"
	"github.com/l3ony2k/twinnel/core/internal/rulegen"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

// bigSubscription is a synthetic subscription with n rules (rulegen's
// shape), the groups those rules name, and a few made-up nodes. RULE-SET,
// GEOIP and IP-ASN lines are left out so a real mihomo -t can check it
// without providers or geo databases.
func bigSubscription(n int) string {
	var b strings.Builder
	b.WriteString("# synthetic subscription for benchmarks\nmixed-port: 7890\nmode: rule\nlog-level: info\nexternal-controller: 127.0.0.1:9090\nproxies:\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "  - {name: node-%02d, type: ss, server: 192.0.2.%d, port: 8388, cipher: aes-128-gcm, password: example}\n", i, i+1)
	}
	b.WriteString("proxy-groups:\n")
	for _, g := range []string{"PROXY", "Streaming", "AI", "Apple", "Games"} {
		fmt.Fprintf(&b, "  - name: %s\n    type: select\n    proxies: [", g)
		for i := 0; i < 40; i++ {
			fmt.Fprintf(&b, "node-%02d, ", i)
		}
		b.WriteString("DIRECT]\n")
	}
	b.WriteString("rules:\n")
	for _, r := range rulegen.Rules(7, n) {
		if strings.HasPrefix(r, "RULE-SET,") || strings.HasPrefix(r, "GEOIP,") || strings.HasPrefix(r, "IP-ASN,") {
			continue
		}
		b.WriteString("  - " + r + "\n")
	}
	return b.String()
}

// saveBench is an engine with a 10k-rule subscription and a few override
// layers, the shape of the owner's report (a few thousand rules imported,
// then saves of small overrides). validate stands in for mihomo -t.
type saveBench struct {
	e   *Engine
	dir string
}

func newSaveBench(tb testing.TB, rules int, validate func(ctx context.Context, bin string, cfg []byte, opts ...config.ValidateOption) (*config.Validation, error)) *saveBench {
	tb.Helper()
	dir := tb.TempDir()
	s, err := store.Open(filepath.Join(dir, "state"))
	if err != nil {
		tb.Fatal(err)
	}
	bin, err := enginetest.WriteFakeMihomo(dir)
	if err != nil {
		tb.Fatal(err)
	}
	confDir := filepath.Join(dir, "conf")
	os.MkdirAll(confDir, 0o755)
	if _, err := s.UpdateSettings(func(st *store.Settings) error {
		st.Mihomo.Binary = bin
		st.Mihomo.ConfigDir = confDir
		st.Mihomo.ConfigFile = filepath.Join(confDir, "config.yaml")
		st.Mihomo.Controller = "127.0.0.1:1" // nothing answers: apply writes and skips the reload
		st.LeakGuardGroups = []string{"AI"}
		return nil
	}); err != nil {
		tb.Fatal(err)
	}
	sec := &store.MemSecrets{}
	sec.Set(store.SecretController, testSecret)
	e := New(s, sec)
	e.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	e.Validate = validate
	ctx := context.Background()
	if _, err := e.PutSubscription(ctx, rpc.SubPutParams{ID: "main", Name: "Main", Content: bigSubscription(rules)}); err != nil {
		tb.Fatal(err)
	}
	base, _ := os.ReadFile("testdata/base.yaml")
	layers := []rpc.LayerPutParams{
		{ID: "base", Stage: "base", Content: string(base)},
		{ID: "lan", Content: "+rules:\n  - DOMAIN-SUFFIX,lan.example,DIRECT\n  - IP-CIDR,192.0.2.0/24,DIRECT,no-resolve\n"},
		{ID: "dns", Content: "dns:\n  nameserver:\n    - https://dns.example/dns-query\n"},
		{ID: "more-rules", Content: "+rules:\n" + strings.Repeat("  - DOMAIN-SUFFIX,more.example,PROXY\n", 1) + bigRulesLayer(500)},
	}
	for _, p := range layers {
		if _, err := e.PutLayer(p); err != nil {
			tb.Fatal(err)
		}
	}
	return &saveBench{e: e, dir: dir}
}

// bigRulesLayer is n rule lines for a "+rules:" layer.
func bigRulesLayer(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  - DOMAIN-SUFFIX,site%d.example,PROXY\n", i)
	}
	return b.String()
}

// okValidate is a mihomo -t that passes at once: the benchmarks measure
// Twinnel's own work.
func okValidate(context.Context, string, []byte, ...config.ValidateOption) (*config.Validation, error) {
	return &config.Validation{OK: true, Output: "test is successful"}, nil
}

const benchRules = 10000

func BenchmarkSaveLayer(b *testing.B) {
	sb := newSaveBench(b, benchRules, okValidate)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := fmt.Sprintf("+rules:\n  - DOMAIN-SUFFIX,lan%d.example,DIRECT\n", i)
		if _, err := sb.e.PutLayer(rpc.LayerPutParams{ID: "lan", Content: c}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLayerList(b *testing.B) {
	sb := newSaveBench(b, benchRules, okValidate)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sb.e.Layers(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompose10k(b *testing.B) {
	sb := newSaveBench(b, benchRules, okValidate)
	st, _ := sb.e.Store.Settings()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := sb.e.compose(context.Background(), st); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPreview10k is compose.preview with mihomo -t stubbed out.
func BenchmarkPreview10k(b *testing.B) {
	sb := newSaveBench(b, benchRules, okValidate)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sb.e.Preview(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReloadImpact10k(b *testing.B) {
	sb := newSaveBench(b, benchRules, okValidate)
	st, _ := sb.e.Store.Settings()
	res, _, err := sb.e.compose(context.Background(), st)
	if err != nil {
		b.Fatal(err)
	}
	next := append([]byte(nil), res.Config...)
	next = append(next, []byte("\n# changed\n")...)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ReloadImpact(res.Config, next)
	}
}

// TestSaveStageTimings logs how long each stage of a save and an apply
// takes on a 10k-rule config (go test -run SaveStageTimings -v). With
// TWINNEL_BENCH_MIHOMO set to a mihomo binary it also times a real
// mihomo -t in a temp dir. It only fails when a stage errors.
func TestSaveStageTimings(t *testing.T) {
	if testing.Short() {
		t.Skip("timings")
	}
	sb := newSaveBench(t, benchRules, okValidate)
	e := sb.e
	ctx := context.Background()
	stage := func(name string, fn func() error) {
		t.Helper()
		start := time.Now()
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%-28s %v", name, time.Since(start).Round(10*time.Microsecond))
	}
	stage("layer.put (small layer)", func() error {
		_, err := e.PutLayer(rpc.LayerPutParams{ID: "lan", Content: "+rules:\n  - DOMAIN-SUFFIX,lan2.example,DIRECT\n"})
		return err
	})
	stage("layer.list", func() error { _, err := e.Layers(); return err })
	stage("settings.get", func() error { _, err := e.Settings(); return err })
	stage("history.list", func() error { _, err := e.History(); return err })
	var st *store.Settings
	stage("read settings", func() error { var err error; st, err = e.Store.Settings(); return err })
	var res *config.Result
	stage("compose (parse+merge)", func() error { var err error; res, _, err = e.compose(ctx, st); return err })
	t.Logf("composed: %d bytes", len(res.Config))
	stage("compose.preview (no -t)", func() error { _, err := e.Preview(ctx); return err })
	stage("compose.apply (no -t)", func() error { _, err := e.Apply(ctx, rpc.ApplyParams{Reason: "bench"}); return err })
	stage("reload impact", func() error {
		ReloadImpact(res.Config, append(append([]byte(nil), res.Config...), "\nmode: global\n"...))
		return nil
	})
	if bin := os.Getenv("TWINNEL_BENCH_MIHOMO"); bin != "" {
		final, _ := withSecret(res.Config, "bench")
		for i := 0; i < 3; i++ {
			stage("mihomo -t (real binary)", func() error {
				v, err := config.Validate(ctx, bin, final)
				if err == nil && !v.OK {
					t.Logf("mihomo -t: %v", v.Errors)
				}
				return err
			})
		}
		// With TWINNEL_BENCH_GEO (a dir holding copies of geo databases)
		// the config also has GEOIP/GEOSITE rules and the test gets the
		// geo files, as the helper's does.
		if geo := os.Getenv("TWINNEL_BENCH_GEO"); geo != "" {
			geoCfg := strings.Replace(string(final), "rules:\n", "rules:\n  - GEOSITE,cn,DIRECT\n  - GEOIP,CN,DIRECT\n", 1)
			for i := 0; i < 3; i++ {
				stage("mihomo -t (geo dir)", func() error {
					v, err := config.Validate(ctx, bin, []byte(geoCfg), config.WithGeoDir(geo))
					if err == nil && !v.OK {
						t.Logf("mihomo -t: %v", v.Errors)
					}
					return err
				})
			}
		}
	}
}
