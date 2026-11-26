package config

import (
	"strings"
	"testing"
)

func TestImportVergeMerge(t *testing.T) {
	tests := []struct {
		name, in, want, wantErr string
	}{
		{
			name: "all sequence keys",
			in: `prepend-rules: [r1]
append-rules: [r2]
prepend-proxies: [{name: p1}]
append-proxies: [{name: p2}]
prepend-proxy-groups: [{name: g1}]
append-proxy-groups: [{name: g2}]
`,
			want: `+rules: [r1]
rules+: [r2]
+proxies: [{name: p1}]
proxies+: [{name: p2}]
+proxy-groups: [{name: g1}]
proxy-groups+: [{name: g2}]
`,
		},
		{
			name: "plain keys first, sequence keys last",
			in:   "append-rules: [z]\nmode: rule\nprepend-rules: [a]\ndns:\n  enable: true\n",
			want: "mode: rule\ndns:\n  enable: true\n+rules: [a]\nrules+: [z]\n",
		},
		{
			name: "marker-like keys are escaped at any depth",
			in:   "dns:\n  nameserver-policy:\n    '+.ts.net': [system]\n    'a!': {x: 1}\n",
			want: "dns:\n  nameserver-policy:\n    '<+.ts.net>': [system]\n    '<a!>': {x: 1}\n",
		},
		{
			name: "plain rules replace",
			in:   "rules: [a]\n",
			want: "rules: [a]\n",
		},
		{
			name: "null sequence key dropped",
			in:   "prepend-rules:\nmode: rule\n",
			want: "mode: rule\n",
		},
		{
			name:    "sequence key with map",
			in:      "prepend-rules: {a: 1}\n",
			wantErr: "prepend-rules: needs a list",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ImportVergeMerge([]byte(tt.in))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if g, w := canon(t, string(got)), canon(t, tt.want); g != w {
				t.Errorf("got:\n%s\nwant:\n%s", g, w)
			}
		})
	}
}

// The converted file must give the same result as Verge would.
func TestImportVergeMergeRoundTrip(t *testing.T) {
	base := "mode: rule\nrules: [MATCH,PROXY]\ndns:\n  enable: false\n  nameserver-policy:\n    '+.lan': [system]\n"
	verge := "prepend-rules: [PROCESS-NAME,tailscaled,DIRECT]\ndns:\n  enable: true\n  nameserver-policy:\n    '+.ts.net': [system]\n"
	conv, err := ImportVergeMerge([]byte(verge))
	if err != nil {
		t.Fatal(err)
	}
	got, err := MergeBytes([]byte(base), conv)
	if err != nil {
		t.Fatal(err)
	}
	want := "mode: rule\nrules: [PROCESS-NAME,tailscaled,DIRECT, MATCH,PROXY]\ndns:\n  enable: true\n  nameserver-policy:\n    '+.lan': [system]\n    '+.ts.net': [system]\n"
	if g, w := canon(t, string(got)), canon(t, want); g != w {
		t.Errorf("got:\n%s\nwant:\n%s", g, w)
	}
}
