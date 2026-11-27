package bundle

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/store"
)

// All data here is synthetic.
const (
	testURL  = "https://sub.example.invalid/api/v1/client?token=synthetic-token"
	testSub  = "proxies:\n  - {name: n1, type: ss, server: 192.0.2.1, port: 8388, cipher: aes-128-gcm, password: synthetic}\nproxy-groups:\n  - {name: PROXY, type: select, proxies: [n1]}\nrules:\n  - MATCH,PROXY\n"
	testRule = "+rules:\n  - DOMAIN-SUFFIX,example.invalid,DIRECT\n"
	testVrg  = "prepend-rules:\n  - DOMAIN,example.invalid,DIRECT\n"
)

func zipOf(t *testing.T, files ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i+1 < len(files); i += 2 {
		w, err := zw.Create(files[i])
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(files[i+1]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestWriteReadRoundTrip(t *testing.T) {
	in := &Bundle{
		Layers: []Layer{
			{ID: "zz-last", Name: "Z", Stage: store.StageUser, Format: store.FormatMerge, Enabled: false, Content: testRule},
			{ID: "a-first", Name: "A 规则", Stage: store.StageUser, Format: store.FormatVerge, Enabled: true, Content: testVrg},
			{ID: "adopted", Name: "Adopted", Stage: store.StageBase, Format: store.FormatMerge, Enabled: true, NameCode: "x", NameArgs: []string{"y"}, Content: "a: 1\n"},
		},
		Subscriptions: []Subscription{
			{ID: "main", Name: "Main", Active: true, IntervalMinutes: 60, URL: testURL, Content: testSub},
			{ID: "spare", Name: "Spare", IntervalMinutes: 0, URL: testURL + "2"},
		},
		Config: "mode: rule\n",
	}
	var buf bytes.Buffer
	when := time.Date(2026, 11, 28, 1, 2, 3, 0, time.UTC)
	if err := Write(&buf, in, when, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	b, err := Parse("whatever.zip", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if b.Source != SourceTwinnel || b.Version != Version || b.App != "1.2.3" || !b.Created.Equal(when) {
		t.Fatalf("header: %+v", b)
	}
	if len(b.Layers) != 3 || b.Layers[0].ID != "zz-last" || b.Layers[1].ID != "a-first" || b.Layers[2].ID != "adopted" {
		t.Fatalf("layer order lost: %v", b.Layers)
	}
	if b.Layers[0].Enabled || !b.Layers[1].Enabled || b.Layers[1].Format != store.FormatVerge || b.Layers[1].Name != "A 规则" {
		t.Fatalf("layer meta lost: %+v", b.Layers[:2])
	}
	if b.Layers[2].Stage != store.StageBase || b.Layers[2].NameCode != "x" || b.Layers[0].Content != testRule {
		t.Fatalf("layer: %+v", b.Layers[2])
	}
	if len(b.Subscriptions) != 2 || !b.Subscriptions[0].Active || b.Subscriptions[0].URL != testURL ||
		b.Subscriptions[0].Content != testSub || b.Subscriptions[0].IntervalMinutes != 60 {
		t.Fatalf("subs: %v", b.Subscriptions)
	}
	if b.Subscriptions[1].Content != "" || b.Subscriptions[1].File != "" {
		t.Fatalf("spare has no content: %v", b.Subscriptions[1])
	}
	if b.Config != "mode: rule\n" || !b.HasSecrets() {
		t.Fatal("config / secrets")
	}
	for _, s := range []string{b.String(), fmt.Sprintf("%v %#v %+v", b, b, b)} {
		if strings.Contains(s, "synthetic-token") {
			t.Fatalf("bundle formatting shows a URL: %s", s)
		}
	}
}

func TestReadLegacy(t *testing.T) {
	data := zipOf(t, "config.yaml", testSub, "overrides/second.yaml", testVrg, "overrides/first.yaml", testRule, "__MACOSX/._x", "junk")
	b, err := Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if b.Source != SourceLegacy || b.Version != 0 || len(b.Layers) != 2 {
		t.Fatalf("%+v", b)
	}
	if b.Layers[0].ID != "second" || b.Layers[0].Format != store.FormatVerge || !b.Layers[0].Enabled || b.Layers[0].Stage != store.StageUser {
		t.Fatalf("zip order and detected format: %+v", b.Layers[0])
	}
	if b.Layers[1].Format != store.FormatMerge || len(b.Subscriptions) != 0 || b.Config != testSub {
		t.Fatalf("%+v", b.Layers[1])
	}
}

func TestReadRefuses(t *testing.T) {
	newer := zipOf(t, "manifest.json", `{"format":"twinnel-export","version":99,"layers":[]}`)
	other := zipOf(t, "manifest.json", `{"format":"something","version":1}`)
	missing := zipOf(t, "manifest.json", `{"format":"twinnel-export","version":1,"layers":[{"id":"x","file":"overrides/x.yaml"}]}`)
	badID := zipOf(t, "manifest.json", `{"format":"twinnel-export","version":1,"layers":[{"id":"../x"}]}`)
	dup := zipOf(t, "manifest.json", `{"format":"twinnel-export","version":1,"layers":[{"id":"x"},{"id":"x"}]}`, "overrides/x.yaml", "a: 1\n")
	noSrc := zipOf(t, "manifest.json", `{"format":"twinnel-export","version":1,"layers":[],"subscriptions":[{"id":"s"}]}`)
	trav := zipOf(t, "../evil.yaml", "a: 1\n")
	abs := zipOf(t, "/etc/evil.yaml", "a: 1\n")
	empty := zipOf(t, "readme.txt", "hi")
	legacyBad := zipOf(t, "overrides/Bad ID.yaml", "a: 1\n")
	for name, data := range map[string][]byte{"newer": newer, "other": other, "missing": missing, "badID": badID, "dup": dup,
		"noSrc": noSrc, "trav": trav, "abs": abs, "empty": empty, "legacyBad": legacyBad, "notzip": []byte("PK\x03\x04garbage")} {
		if _, err := Parse("x.zip", data); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Parse("x", nil); err == nil {
		t.Error("empty accepted")
	}
	if _, err := Parse("x", make([]byte, MaxFile+1)); err == nil {
		t.Error("oversize accepted")
	}
}

func TestReadBomb(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("overrides/big.yaml")
	chunk := bytes.Repeat([]byte("a"), 1<<20)
	for range MaxEntry>>20 + 1 {
		w.Write(chunk)
	}
	zw.Close()
	if buf.Len() > MaxFile {
		t.Skip("compressed bomb larger than MaxFile")
	}
	if _, err := Read(buf.Bytes()); err == nil || !strings.Contains(err.Error(), "MiB") {
		t.Fatalf("bomb: %v", err)
	}
}

func TestParseURLList(t *testing.T) {
	text := "# my subscriptions\n\n" + testURL + "\nWork  https://b.example.invalid/x?a=1,2#ignored\nhttps://c.example.invalid/sub#Home%20Net\n" + testURL + "\n"
	b, err := Parse("subs.txt", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if b.Source != SourceURLs || len(b.Subscriptions) != 4 {
		t.Fatalf("%v", b)
	}
	s := b.Subscriptions
	if !s[0].Active || s[1].Active || s[0].ID != "sub-example-invalid" || s[0].URL != testURL {
		t.Fatalf("first: %+v", s[0])
	}
	if s[1].Name != "Work" || s[1].ID != "work" || s[1].URL != "https://b.example.invalid/x?a=1,2#ignored" {
		t.Fatalf("named: %+v", s[1])
	}
	if s[2].Name != "Home Net" || s[2].ID != "home-net" {
		t.Fatalf("fragment: %+v", s[2])
	}
	if s[3].ID != "sub-example-invalid-2" || s[3].IntervalMinutes != 1440 {
		t.Fatalf("dup id: %+v", s[3])
	}
	// A line that is not a URL makes it something else (here: YAML).
	if b, err := Parse("x.txt", []byte(testURL+"\nfoo: bar\n")); err == nil && b.Source == SourceURLs {
		t.Fatal("mixed text read as a URL list")
	}
	if _, err := Parse("x.txt", []byte("ftp://a.example.invalid/x\n")); err == nil {
		t.Fatal("ftp accepted")
	}
}

func TestParseOverrideAndConfig(t *testing.T) {
	b, err := Parse("/tmp/My Rules.yaml", []byte(testVrg))
	if err != nil {
		t.Fatal(err)
	}
	if b.Source != SourceOverride || len(b.Layers) != 1 || b.Layers[0].ID != "my-rules" || b.Layers[0].Name != "My Rules" ||
		b.Layers[0].Format != store.FormatVerge || !b.Layers[0].Enabled {
		t.Fatalf("%+v", b.Layers)
	}
	b, err = Parse("party.yml", []byte(testRule))
	if err != nil || b.Layers[0].Format != store.FormatMerge || b.Layers[0].ID != "party" {
		t.Fatalf("%v %+v", err, b)
	}
	b, err = Parse("机场.yaml", []byte(testSub))
	if err != nil || b.Source != SourceConfig || len(b.Subscriptions) != 1 || b.Subscriptions[0].ID != "imported" ||
		b.Subscriptions[0].Content != testSub || b.Subscriptions[0].URL != "" {
		t.Fatalf("%v %+v", err, b)
	}
	for _, bad := range []string{"function main(config) { return config }\n", "- a\n- b\n", "# only a comment\n"} {
		if _, err := Parse("x.js", []byte(bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := Parse("x.bin", []byte{0x01, 0x00, 0x02}); err == nil {
		t.Error("binary accepted")
	}
}

func TestSlugAndIDs(t *testing.T) {
	for in, want := range map[string]string{"My Rules": "my-rules", "  --x--  ": "x", "机场": "fb", "a.b_c": "a-b_c",
		strings.Repeat("a", 80): strings.Repeat("a", 64)} {
		if got := Slug(in, "fb"); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	taken := map[string]bool{"x": true, "x-2": true}
	if got := FreeID("x", func(s string) bool { return taken[s] }); got != "x-3" {
		t.Errorf("FreeID = %q", got)
	}
	long := strings.Repeat("b", 64)
	if got := FreeID(long, func(string) bool { return false }); len(got) > 64 || !store.ValidID(got) {
		t.Errorf("FreeID long = %q", got)
	}
}
