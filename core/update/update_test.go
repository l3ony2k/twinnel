package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.0", true},
		{"v0.1.1", "0.1.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.1.0-dev", true}, // the release of a dev build
		{"0.1.0-rc2", "0.1.0-rc1", true},
		{"0.1.0-rc1", "0.1.0", false},
		{"0.0.9", "0.1.0-dev", false},
		{"1", "0.9.9", true},
		{"1.0.0", "1.0.0-3-gabc1234", true}, // a build past a tag is pre-release-shaped
		{"x.y", "0.1.0", false},
		{"0.1.0", "dev", true},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

// server answers the latest-release request and records what it got.
func server(t *testing.T, status int, body string, hdr map[string]string) (*httptest.Server, *http.Request) {
	t.Helper()
	got := new(http.Request)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = *r.Clone(context.Background())
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func check(t *testing.T, srv *httptest.Server, current string) (Result, error) {
	t.Helper()
	return Check(context.Background(), Options{API: srv.URL, Module: "github.com/owner/repo", Current: current,
		Proxy: func(*http.Request) (*url.URL, error) { return nil, nil }})
}

func TestCheckAvailable(t *testing.T) {
	srv, req := server(t, 200, `{"tag_name":"v0.3.0","html_url":"https://github.com/owner/repo/releases/tag/v0.3.0","draft":false}`, nil)
	r, err := check(t, srv, "0.2.1")
	if err != nil || r.State != Available || r.Latest != "0.3.0" || r.URL != "https://github.com/owner/repo/releases/tag/v0.3.0" {
		t.Fatalf("%+v %v", r, err)
	}
	if req.URL.Path != "/repos/owner/repo/releases/latest" || req.Method != http.MethodGet {
		t.Errorf("request %s %s", req.Method, req.URL)
	}
	// Nothing identifying: a generic agent, no credentials, no cookies.
	if req.Header.Get("User-Agent") != "duet" || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.URL.RawQuery != "" {
		t.Errorf("headers %v query %q", req.Header, req.URL.RawQuery)
	}
}

func TestCheckUpToDate(t *testing.T) {
	srv, _ := server(t, 200, `{"tag_name":"v0.2.1","html_url":"https://github.com/owner/repo/releases/tag/v0.2.1"}`, nil)
	if r, err := check(t, srv, "0.2.1"); err != nil || r.State != UpToDate {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestCheckNoReleases(t *testing.T) {
	// A private or unpublished repository answers 404, as one without
	// releases does: a calm state, not an error.
	srv, _ := server(t, 404, `{"message":"Not Found"}`, nil)
	if r, err := check(t, srv, "0.1.0-dev"); err != nil || r.State != NoReleases {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestCheckErrors(t *testing.T) {
	srv, _ := server(t, 403, `{"message":"API rate limit exceeded"}`, map[string]string{"X-RateLimit-Remaining": "0"})
	if _, err := check(t, srv, "0.1.0"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("rate limit: %v", err)
	}
	srv, _ = server(t, 500, ``, nil)
	if _, err := check(t, srv, "0.1.0"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("500: %v", err)
	}
	srv, _ = server(t, 200, `not json`, nil)
	if _, err := check(t, srv, "0.1.0"); err == nil {
		t.Error("bad json accepted")
	}
	// An odd release page URL is replaced by the repository's.
	srv, _ = server(t, 200, `{"tag_name":"v9.0.0","html_url":"https://evil.example/x"}`, nil)
	if r, err := check(t, srv, "0.1.0"); err != nil || !strings.HasPrefix(r.URL, "https://github.com/") {
		t.Errorf("url %q %v", r.URL, err)
	}
	if _, err := Check(context.Background(), Options{API: srv.URL, Module: "example.com/x"}); !errors.Is(err, ErrBadModule) {
		t.Errorf("bad module: %v", err)
	}
}

// The request goes through the proxy the options name.
func TestCheckProxy(t *testing.T) {
	hits := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			t.Errorf("proxied %s", r.URL)
		}
		w.WriteHeader(404)
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	r, err := Check(context.Background(), Options{API: "http://api.invalid", Module: "github.com/o/r", Current: "0.1.0",
		Proxy: http.ProxyURL(pu)})
	if err != nil || r.State != NoReleases || hits != 1 {
		t.Errorf("%+v %v hits %d", r, err, hits)
	}
}
