// Package version is Twinnel's own version and module path, and the version
// report of every component (`twinnel version`, the app's 关于 page).
package version

import (
	"runtime/debug"
	"strings"
)

// Module is Twinnel's Go module path: the GitHub repository it is published
// from (releases, issues, the license). The one place it is written down.
const Module = "github.com/l3ony2k/twinnel"

// Version is Twinnel's version, set at build time (scripts/build-release.sh):
//
//	-ldflags "-X github.com/l3ony2k/twinnel/core/version.Version=1.2.3"
var Version = "0.1.0-dev"

// RepoURL is the repository's web page.
func RepoURL() string { return "https://" + Module }

// Owner and Repo split Module's GitHub path ("l3ony2k", "twinnel").
func Owner() string { return part(1) }
func Repo() string  { return part(2) }

func part(i int) string {
	p := strings.Split(Module, "/")
	if i < len(p) {
		return p[i]
	}
	return ""
}

// IsDev reports a development build: no release version was linked in.
func IsDev() bool { return Version == "" || strings.Contains(Version, "dev") }

// String is the version to show: Version, and for a development build the
// commit it was built from when Go recorded one ("0.1.0-dev (a1b2c3d)",
// "-dirty" with uncommitted changes).
func String() string {
	v := Version
	if v == "" {
		v = "dev"
	}
	if !IsDev() {
		return v
	}
	if rev := revision(); rev != "" {
		return v + " (" + rev + ")"
	}
	return v
}

// revision reads the VCS stamp of the build (go build in a checkout).
var revision = func() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev string
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if rev != "" && dirty {
		rev += "-dirty"
	}
	return rev
}

// Pages of the repository the app links to.
func ReleasesURL() string { return RepoURL() + "/releases" }
func LicenseURL() string  { return RepoURL() + "/blob/main/LICENSE" }
func NoticesURL() string  { return RepoURL() + "/blob/main/THIRD_PARTY_NOTICES.md" }
func IssuesURL() string   { return RepoURL() + "/issues" }
