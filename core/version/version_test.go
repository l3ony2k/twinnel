package version

import "testing"

func TestString(t *testing.T) {
	oldV, oldR := Version, revision
	t.Cleanup(func() { Version, revision = oldV, oldR })
	revision = func() string { return "a1b2c3d" }
	Version = "0.1.0-dev"
	if got := String(); got != "0.1.0-dev (a1b2c3d)" || !IsDev() {
		t.Errorf("dev: %q", got)
	}
	Version = "1.2.3"
	if got := String(); got != "1.2.3" || IsDev() {
		t.Errorf("release: %q", got)
	}
	revision = func() string { return "" }
	Version = ""
	if got := String(); got != "dev" {
		t.Errorf("empty: %q", got)
	}
	if Owner() != "l3ony2k" || Repo() != "twinnel" || RepoURL() != "https://github.com/l3ony2k/twinnel" {
		t.Errorf("repo %s %s %s", Owner(), Repo(), RepoURL())
	}
}
