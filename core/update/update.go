// Package update checks GitHub Releases for a newer duet. It only reads:
// one unauthenticated GET, nothing is downloaded or installed.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/version"
)

// State is the outcome of a check.
type State int

const (
	UpToDate   State = iota + 1 // the latest release is not newer
	Available                   // a newer release exists
	NoReleases                  // the repository has no release (or is not public yet)
)

// Result is a check's outcome.
type Result struct {
	State   State
	Latest  string // the latest release's version, without a leading "v"
	URL     string // its release page
	Current string
}

// Options configure a check. Zero values use the defaults.
type Options struct {
	// API is the GitHub API base URL (tests); default https://api.github.com.
	API string
	// Module is the repository's module path; default version.Module.
	Module string
	// Current is the running version; default version.Version.
	Current string
	// Proxy picks the proxy for the request (the system's on macOS);
	// default http.ProxyFromEnvironment.
	Proxy func(*http.Request) (*url.URL, error)
	// Timeout bounds the whole request; default 15s.
	Timeout time.Duration
}

// UserAgent is the only identification sent: no version, user or machine.
const UserAgent = "duet"

// Errors a check returns besides network errors.
var (
	ErrRateLimited = errors.New("GitHub rate limit reached")
	ErrBadModule   = errors.New("module path is not a GitHub repository")
)

// Check asks GitHub for the latest release of the repository.
func Check(ctx context.Context, o Options) (Result, error) {
	if o.API == "" {
		o.API = "https://api.github.com"
	}
	if o.Module == "" {
		o.Module = version.Module
	}
	if o.Current == "" {
		o.Current = version.Version
	}
	if o.Proxy == nil {
		o.Proxy = http.ProxyFromEnvironment
	}
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Second
	}
	res := Result{Current: o.Current}
	p := strings.Split(o.Module, "/")
	if len(p) != 3 || p[0] != "github.com" || p[1] == "" || p[2] == "" {
		return res, ErrBadModule
	}
	u := strings.TrimRight(o.API, "/") + "/repos/" + url.PathEscape(p[1]) + "/" + url.PathEscape(p[2]) + "/releases/latest"
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return res, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Transport: &http.Transport{Proxy: o.Proxy, ForceAttemptHTTP2: true}}
	resp, err := client.Do(req)
	if err != nil {
		return res, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		// No release yet, or the repository is private / unpublished.
		res.State = NoReleases
		return res, nil
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.StatusCode == http.StatusTooManyRequests {
			return res, ErrRateLimited
		}
		return res, fmt.Errorf("HTTP %d", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return res, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Draft   bool   `json:"draft"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return res, fmt.Errorf("bad answer: %w", err)
	}
	if rel.TagName == "" || rel.Draft {
		res.State = NoReleases
		return res, nil
	}
	res.Latest = strings.TrimPrefix(rel.TagName, "v")
	res.URL = rel.HTMLURL
	if !strings.HasPrefix(res.URL, "https://github.com/") {
		res.URL = version.RepoURL() + "/releases/latest"
	}
	if Newer(res.Latest, o.Current) {
		res.State = Available
	} else {
		res.State = UpToDate
	}
	return res, nil
}

// Newer reports whether version a is newer than b. Versions are
// "major.minor.patch[-pre]" with an optional "v"; a missing part is 0. A
// pre-release ("0.2.0-dev", "0.2.0-rc1", a `git describe` suffix) is older
// than its release. What does not parse is never newer.
func Newer(a, b string) bool {
	va, pa, ok := parse(a)
	if !ok {
		return false
	}
	vb, pb, ok := parse(b)
	if !ok {
		return true // a dev build without a version: any release is newer
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	switch {
	case pa == "" && pb != "":
		return true
	case pa != "" && pb == "":
		return false
	}
	return pa > pb
}

func parse(v string) ([3]int, string, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	core, pre, _ := strings.Cut(v, "-")
	if i := strings.IndexByte(core, '+'); i >= 0 {
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	if core == "" || len(parts) > 3 {
		return out, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, "", false
		}
		out[i] = n
	}
	return out, pre, true
}
