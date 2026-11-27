package status

import "testing"

func TestLintOverride(t *testing.T) {
	src := `+rules:
  - DOMAIN,a.com,DIRECT
proxies+:
  - {name: x, type: direct}
dns!:
  enable: true
hosts:
  +.lan: 10.0.0.1
  '<+.home>': 10.0.0.2
  plain.example: 10.0.0.3
nameserver-policy:
  '*.corp!': 1.1.1.1
dns-extra:
  +nameserver: 8.8.8.8
`
	notes := LintOverride(src)
	got := map[string]Level{}
	lines := map[string]int{}
	for _, n := range notes {
		got[n.Key] = n.Level
		lines[n.Key] = n.Line
	}
	want := map[string]Level{
		"+rules":      LevelOK,
		"proxies+":    LevelOK,
		"dns!":        LevelOK,
		"+.lan":       LevelError, // needs a list (refused), and looks like a domain
		"*.corp!":     LevelWarn, // domain-like
		"+nameserver": LevelError, // needs a list (refused)
	}
	if len(got) != len(want) {
		t.Fatalf("notes = %+v", notes)
	}
	for k, l := range want {
		if g, ok := got[k]; !ok || g != l {
			t.Errorf("%s: level %v (present %v), want %v", k, g, ok, l)
		}
	}
	if lines["+.lan"] != 8 {
		t.Errorf("+.lan line = %d, want 8", lines["+.lan"])
	}
}

func TestLintOverrideYAMLError(t *testing.T) {
	n := LintOverride("a: [1,\n")
	if len(n) != 1 || n[0].Level != LevelError {
		t.Fatalf("notes = %+v", n)
	}
	if n := LintOverride(""); len(n) != 0 {
		t.Fatalf("empty: %+v", n)
	}
}
