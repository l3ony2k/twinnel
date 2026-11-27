package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

func TestParseLaunchOpen(t *testing.T) {
	cases := []struct {
		v    string
		want launchOpen
		err  bool
	}{
		{"", launchOpen{}, false},
		{"0", launchOpen{}, false},
		{"1", launchOpen{main: true}, false},
		{"panel", launchOpen{main: true, panel: true}, false},
		{"page:kernel", launchOpen{main: true, page: pageKernel}, false},
		{" page: takeover ", launchOpen{main: true, page: pageTakeover}, false},
		{"page:wizard", launchOpen{main: true, page: pageWizard}, false},
		{"page:nosuch", launchOpen{main: true}, true},
		{"page:", launchOpen{main: true}, true},
	}
	for _, c := range cases {
		got, err := parseLaunchOpen(c.v)
		if got != c.want || (err != nil) != c.err {
			t.Errorf("%q: got %+v %v, want %+v err=%v", c.v, got, err, c.want, c.err)
		}
	}
}

// --open wins over TWINNEL_OPEN, in either spelling.
func TestLaunchRequest(t *testing.T) {
	getenv := env(map[string]string{envOpen: "page:health"})
	cases := []struct {
		args []string
		want launchOpen
	}{
		{nil, launchOpen{main: true, page: pageHealth}},
		{[]string{"--open=page:kernel"}, launchOpen{main: true, page: pageKernel}},
		{[]string{"-psn_0_1", "--open", "page:logs"}, launchOpen{main: true, page: pageLogs}},
		{[]string{"--open=0"}, launchOpen{}},
	}
	for _, c := range cases {
		if got, _ := launchRequest(c.args, getenv); got != c.want {
			t.Errorf("%v: got %+v, want %+v", c.args, got, c.want)
		}
	}
	if got, _ := launchRequest(nil, env(nil)); got != (launchOpen{}) {
		t.Errorf("a normal launch opens nothing itself: %+v", got)
	}
}

// Every page constant is in allPages, so TWINNEL_OPEN=page:<id> reaches it.
func TestAllPagesListed(t *testing.T) {
	re := regexp.MustCompile(`(?m)^\s*(?:const\s+)?page[A-Z]\w*\s*=\s*"([a-z-]+)"`)
	files, _ := filepath.Glob("*.go")
	found := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			found++
			if !slices.Contains(allPages, m[1]) {
				t.Errorf("%s: page %q is not in allPages", f, m[1])
			}
		}
	}
	if found < len(allPages) {
		t.Errorf("found %d page constants, allPages has %d", found, len(allPages))
	}
}
