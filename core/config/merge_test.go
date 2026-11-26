package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// canon re-encodes a YAML document so that tests compare structure and key
// order, not formatting.
func canon(t *testing.T, s string) string {
	t.Helper()
	n, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	stripStyle(n)
	out, err := Encode(n)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func stripStyle(n *yaml.Node) {
	n.Style = 0
	for _, c := range n.Content {
		stripStyle(c)
	}
}

func TestMerge(t *testing.T) {
	tests := []struct {
		name, base, patch, want string
		wantErr                 string
	}{
		{
			name:  "scalar replace keeps position",
			base:  "a: 1\nb: 2\nc: 3\n",
			patch: "b: 20\n",
			want:  "a: 1\nb: 20\nc: 3\n",
		},
		{
			name:  "new keys append at end",
			base:  "a: 1\n",
			patch: "z: 26\nb: 2\n",
			want:  "a: 1\nz: 26\nb: 2\n",
		},
		{
			name:  "map deep merge",
			base:  "dns:\n  enable: false\n  listen: 0.0.0.0:53\n",
			patch: "dns:\n  enable: true\n  ipv6: false\n",
			want:  "dns:\n  enable: true\n  listen: 0.0.0.0:53\n  ipv6: false\n",
		},
		{
			name:  "nested deep merge three levels",
			base:  "a:\n  b:\n    c: 1\n    d: 2\n",
			patch: "a:\n  b:\n    d: 3\n    e: 4\n",
			want:  "a:\n  b:\n    c: 1\n    d: 3\n    e: 4\n",
		},
		{
			name:  "map replace with bang",
			base:  "dns:\n  enable: false\n  listen: 0.0.0.0:53\n",
			patch: "dns!:\n  enable: true\n",
			want:  "dns:\n  enable: true\n",
		},
		{
			name:  "nested map replace with bang",
			base:  "dns:\n  enable: true\n  nameserver-policy:\n    a.com: [1.1.1.1]\n",
			patch: "dns:\n  nameserver-policy!:\n    b.com: [8.8.8.8]\n",
			want:  "dns:\n  enable: true\n  nameserver-policy:\n    b.com: [8.8.8.8]\n",
		},
		{
			name:  "bang on list and scalar",
			base:  "rules: [a]\nmode: rule\n",
			patch: "rules!: [b]\nmode!: global\n",
			want:  "rules: [b]\nmode: global\n",
		},
		{
			name:  "array replaces by default",
			base:  "rules:\n  - a\n  - b\n",
			patch: "rules:\n  - c\n",
			want:  "rules:\n  - c\n",
		},
		{
			name:  "prepend",
			base:  "rules:\n  - a\n  - b\n",
			patch: "+rules:\n  - x\n  - y\n",
			want:  "rules:\n  - x\n  - y\n  - a\n  - b\n",
		},
		{
			name:  "append",
			base:  "rules:\n  - a\n  - b\n",
			patch: "rules+:\n  - x\n",
			want:  "rules:\n  - a\n  - b\n  - x\n",
		},
		{
			name:  "prepend and append to missing key",
			base:  "a: 1\n",
			patch: "+rules: [x]\nproxies+: [p]\n",
			want:  "a: 1\nrules: [x]\nproxies: [p]\n",
		},
		{
			name:  "prepend onto scalar treats it as empty",
			base:  "rules: none\n",
			patch: "+rules: [x]\n",
			want:  "rules: [x]\n",
		},
		{
			name:  "prepend and append in same patch",
			base:  "rules: [m]\n",
			patch: "+rules: [a]\nrules+: [z]\n",
			want:  "rules: [a, m, z]\n",
		},
		{
			name:  "replace then append, in document order",
			base:  "rules: [old]\n",
			patch: "rules: [new]\nrules+: [z]\n",
			want:  "rules: [new, z]\n",
		},
		{
			name:  "nested prepend and append",
			base:  "dns:\n  fake-ip-filter: ['*.lan']\ntun:\n  route-exclude-address: [10.0.0.0/8]\n",
			patch: "dns:\n  fake-ip-filter+: ['+.ts.net']\ntun:\n  +route-exclude-address: [100.64.0.0/10]\n",
			want:  "dns:\n  fake-ip-filter: ['*.lan', '+.ts.net']\ntun:\n  route-exclude-address: [100.64.0.0/10, 10.0.0.0/8]\n",
		},
		{
			name:  "angle brackets make a plus key literal",
			base:  "dns:\n  nameserver-policy:\n    '+.lan': [system]\n",
			patch: "dns:\n  nameserver-policy:\n    '<+.ts.net>': [system]\n",
			want:  "dns:\n  nameserver-policy:\n    '+.lan': [system]\n    '+.ts.net': [system]\n",
		},
		{
			name:  "unwrapped plus key is a prepend (clash party behaviour)",
			base:  "dns:\n  nameserver-policy: {}\n",
			patch: "dns:\n  nameserver-policy:\n    '+.ts.net': [system]\n",
			want:  "dns:\n  nameserver-policy:\n    .ts.net: [system]\n",
		},
		{
			name:  "angle brackets replace an existing literal plus key",
			base:  "p:\n  '+.ts.net': [a]\n",
			patch: "p:\n  '<+.ts.net>': [b]\n",
			want:  "p:\n  '+.ts.net': [b]\n",
		},
		{
			name:  "markers outside brackets",
			base:  "p:\n  'a+': [1]\n  'b!': {x: 1}\n",
			patch: "p:\n  '<a+>+': [2]\n  '<b!>!': {y: 2}\n",
			want:  "p:\n  'a+': [1, 2]\n  'b!': {y: 2}\n",
		},
		{
			name:  "angle brackets on map key merge",
			base:  "p:\n  'x!':\n    a: 1\n",
			patch: "p:\n  '<x!>':\n    b: 2\n",
			want:  "p:\n  'x!':\n    a: 1\n    b: 2\n",
		},
		{
			name:  "map merged onto scalar replaces it",
			base:  "dns: off\n",
			patch: "dns:\n  enable: true\n",
			want:  "dns:\n  enable: true\n",
		},
		{
			name:  "list replaced by map",
			base:  "a: [1]\n",
			patch: "a:\n  b: 1\n",
			want:  "a:\n  b: 1\n",
		},
		{
			name:  "markers resolved inside new map",
			base:  "a: 1\n",
			patch: "tun:\n  route-exclude-address+: [100.64.0.0/10]\n  enable: true\n",
			want:  "a: 1\ntun:\n  route-exclude-address: [100.64.0.0/10]\n  enable: true\n",
		},
		{
			name:  "markers resolved inside bang replacement",
			base:  "tun:\n  enable: false\n",
			patch: "tun!:\n  +dns-hijack: [any:53]\n  '<a+>': 1\n",
			want:  "tun:\n  dns-hijack: [any:53]\n  'a+': 1\n",
		},
		{
			name:  "list items are data, not overrides",
			base:  "proxies: []\n",
			patch: "proxies+:\n  - name: p\n    'x+': [1]\n",
			want:  "proxies:\n  - name: p\n    'x+': [1]\n",
		},
		{
			name:  "null scalar replaces",
			base:  "a: 1\n",
			patch: "a: null\n",
			want:  "a: null\n",
		},
		{
			name:  "null prepend is a no-op",
			base:  "rules: [a]\n",
			patch: "+rules:\n",
			want:  "rules: [a]\n",
		},
		{
			name:  "empty patch",
			base:  "a: 1\n",
			patch: "# only a comment\n",
			want:  "a: 1\n",
		},
		{
			name:  "empty base",
			base:  "",
			patch: "+rules: [a]\n",
			want:  "rules: [a]\n",
		},
		{
			name:  "single-character keys are literal",
			base:  "a: 1\n",
			patch: "'+': 1\n'!': 2\n",
			want:  "a: 1\n'+': 1\n'!': 2\n",
		},
		{
			name:  "aliases in patch are expanded",
			base:  "a: 1\n",
			patch: "x: &d [1, 2]\n+rules: *d\n",
			want:  "a: 1\nx: [1, 2]\nrules: [1, 2]\n",
		},
		{
			name:  "anchors in base survive replacement of their source",
			base:  "g: &g [a, b]\nh: *g\n",
			patch: "g: [c]\n",
			want:  "g: [c]\nh: [a, b]\n",
		},
		{
			name:  "merge keys in base are folded",
			base:  "d: &d {x: 1, y: 2}\ne:\n  <<: *d\n  y: 3\n",
			patch: "e:\n  z: 4\n",
			want:  "d: {x: 1, y: 2}\ne:\n  y: 3\n  x: 1\n  z: 4\n",
		},
		{
			name:    "prepend with a non-list",
			base:    "a: 1\n",
			patch:   "dns:\n  +nameserver: 1.1.1.1\n",
			wantErr: `dns.nameserver: "+nameserver" needs a list`,
		},
		{
			name:    "top level not a mapping",
			base:    "a: 1\n",
			patch:   "- a\n",
			wantErr: "top level must be a mapping",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergeBytes([]byte(tt.base), []byte(tt.patch))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, want := canon(t, string(got)), canon(t, tt.want); got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestMergeKeepsComments(t *testing.T) {
	base := "# head\nport: 7890 # the port\n# rules below\nrules:\n  - a # first\n"
	patch := "port: 7891\nrules+:\n  - b # added\n"
	got, err := MergeBytes([]byte(base), []byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"# head", "# rules below", "# first", "# added"} {
		if !strings.Contains(string(got), s) {
			t.Errorf("missing %q in:\n%s", s, got)
		}
	}
}

func TestMergeDoesNotModifyPatch(t *testing.T) {
	b, _ := Parse([]byte("rules: [a]\n"))
	p, _ := Parse([]byte("+rules: [x]\n"))
	before, _ := Encode(p)
	if err := Merge(b, p); err != nil {
		t.Fatal(err)
	}
	b2, _ := Parse([]byte("rules: [a]\n"))
	if err := Merge(b2, p); err != nil {
		t.Fatal(err)
	}
	after, _ := Encode(p)
	if string(before) != string(after) {
		t.Errorf("patch changed:\n%s\n->\n%s", before, after)
	}
	out, _ := Encode(b2)
	if canon(t, string(out)) != canon(t, "rules: [x, a]\n") {
		t.Errorf("second merge: %s", out)
	}
}

func TestParseRejectsDuplicateKeys(t *testing.T) {
	for _, s := range []string{"a: 1\na: 2\n", "dns:\n  x: 1\n  x: 2\n", "l:\n  - {n: 1, n: 2}\n"} {
		if _, err := Parse([]byte(s)); err == nil || !strings.Contains(err.Error(), "already defined") {
			t.Errorf("%q: err = %v", s, err)
		}
	}
	if _, err := Parse([]byte("b: &b {x: 1}\nc:\n  <<: *b\n  <<: {y: 2}\n  x: 3\n")); err != nil {
		t.Errorf("merge keys: %v", err)
	}
}
