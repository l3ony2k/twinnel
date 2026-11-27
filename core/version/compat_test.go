package version

import "testing"

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
		ok   bool
	}{
		{"0.2.0", "0.1.9", 1, true},
		{"v0.2.0", "0.2.0", 0, true},
		{"0.2.0-dev", "0.2.0", -1, true},
		{"0.2.0-3-gabc1234", "0.2.0", 1, true},
		{"0.2.0-3-gabc1234", "0.2.0-10-gdef5678", -1, true},
		{"0.2.0-10-gdef5678", "0.3.0-rc1", -1, true},
		{"0.2.0-3-gabc1234", "0.2.0-3-gabc1234", 0, true},
		{"0.2.0-3-gabc1234-dirty", "0.2.0-3-gabc1234", 0, false},
		{"0.2.0-dirty", "0.2.0", 1, true},
		{"abc1234", "0.2.0", 0, false},
		{"abc1234", "abc1234", 0, true},
		{"", "", 0, false},
	} {
		got, ok := Compare(c.a, c.b)
		if got != c.want || ok != c.ok {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d, %v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func TestHandshake(t *testing.T) {
	for _, c := range []struct {
		app, helper string
		want        Match
	}{
		{"0.1.0-dev", "0.0.1", MatchSame}, // development app: no version state
		{"0.2.0", "0.2.0", MatchSame},
		{"0.2.0", "0.1.0", MatchHelperOlder},
		{"0.2.0", "0.3.0", MatchHelperNewer},
		{"0.2.0", "", MatchUnknown},
		{"0.2.0-3-gabc-dirty", "0.2.0-3-gdef", MatchHelperOlder},
	} {
		if got := Handshake(c.app, c.helper); got != c.want {
			t.Errorf("Handshake(%q, %q) = %d, want %d", c.app, c.helper, got, c.want)
		}
	}
}
