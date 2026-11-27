package version

import (
	"strconv"
	"strings"
)

// Compare orders two duet versions: -1 when a is older than b, 0 when they
// are the same, 1 when a is newer. Versions are "major.minor.patch" with an
// optional "v" and a suffix: a `git describe` suffix ("-3-gabc1234",
// "-dirty") counts commits after the release, so 0.2.0-3-gabc is newer
// than 0.2.0 and older than 0.2.0-10-gdef; any other suffix ("-dev",
// "-rc1") is a pre-release, older than the release. ok is false when either
// does not parse (then they cannot be ordered).
func Compare(a, b string) (cmp int, ok bool) {
	pa, okA := parseVersion(a)
	pb, okB := parseVersion(b)
	if !okA || !okB {
		if strings.TrimSpace(a) == strings.TrimSpace(b) && a != "" {
			return 0, true
		}
		return 0, false
	}
	for i := range pa.num {
		if pa.num[i] != pb.num[i] {
			return sign(pa.num[i] - pb.num[i]), true
		}
	}
	// Same release: a pre-release < the release < commits after it.
	if pa.rank != pb.rank {
		return sign(pa.rank - pb.rank), true
	}
	switch pa.rank {
	case rankAfter:
		if pa.commits != pb.commits {
			return sign(pa.commits - pb.commits), true
		}
		if pa.rest == pb.rest {
			return 0, true
		}
		// Same count, other commit or a dirty tree: different builds that
		// cannot be ordered.
		return 0, false
	case rankPre:
		return strings.Compare(pa.rest, pb.rest), true
	}
	return 0, true
}

const (
	rankPre     = -1 // 0.2.0-rc1, 0.2.0-dev
	rankRelease = 0  // 0.2.0
	rankAfter   = 1  // 0.2.0-3-gabc1234 (git describe)
)

type parsed struct {
	num     [3]int
	rank    int
	commits int
	rest    string
}

func parseVersion(v string) (parsed, bool) {
	var p parsed
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	core, suffix, hasSuffix := strings.Cut(v, "-")
	if i := strings.IndexByte(core, '+'); i >= 0 {
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	if core == "" || len(parts) > 3 {
		return p, false
	}
	for i, s := range parts {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return p, false
		}
		p.num[i] = n
	}
	if !hasSuffix {
		return p, true
	}
	// git describe: <count>-g<hash>[-dirty]
	if n, rest, ok := strings.Cut(suffix, "-"); ok {
		if c, err := strconv.Atoi(n); err == nil && strings.HasPrefix(rest, "g") {
			p.rank, p.commits, p.rest = rankAfter, c, rest
			return p, true
		}
	}
	if suffix == "dirty" {
		p.rank, p.rest = rankAfter, suffix
		return p, true
	}
	p.rank, p.rest = rankPre, suffix
	return p, true
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// Match is how the helper's version relates to the app's.
type Match int

const (
	MatchSame        Match = iota // same version, or a development app (any helper goes)
	MatchHelperOlder              // the helper needs an update
	MatchHelperNewer              // the app is older than its helper
	MatchUnknown                  // no helper version
)

// Handshake compares the app's version (Version, unless given) with the
// helper's. A development build of the app talks to whatever helper is
// installed: it reports MatchSame, so running from source never shows a
// version state.
func Handshake(app, helper string) Match {
	if app == "" {
		app = Version
	}
	if app == "" || strings.Contains(app, "dev") {
		return MatchSame
	}
	if helper == "" {
		return MatchUnknown
	}
	c, ok := Compare(helper, app)
	switch {
	case !ok:
		// Two builds that cannot be ordered (same commit count, another
		// commit or a dirty tree): the app's own helper is the one to run.
		return MatchHelperOlder
	case c < 0:
		return MatchHelperOlder
	case c > 0:
		return MatchHelperNewer
	}
	return MatchSame
}
