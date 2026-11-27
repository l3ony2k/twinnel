package mihomo

import (
	"net/url"
	"strings"
)

// DashboardURL is the setup page of the dashboard (external UI) mihomo
// serves at path under the controller at base (http://host:port), with
// the controller's hostname and port prefilled. The secret is never put
// in the URL: it would stay in the browser's history; the person pastes
// it into the dashboard's sign-in instead.
func DashboardURL(base, path string) string {
	if path == "" {
		path = "/ui/"
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	base = strings.TrimSuffix(base, "/")
	b, err := url.Parse(base)
	if err != nil || b.Host == "" {
		return base + path
	}
	port := b.Port()
	if port == "" {
		port = "80"
	}
	q := url.Values{"hostname": {b.Hostname()}, "port": {port}}
	return base + path + "#/setup?" + q.Encode()
}
