package i18n

import (
	"net/url"
	"regexp"
	"strings"
)

// Network errors said in words. The helper hands back Go's error text
// (`Get "https://api.github.com/…": proxyconnect tcp: dial tcp
// 127.0.0.1:7890: connect: connection refused`); a page shows a sentence
// that names what could not be reached and why, with the raw text kept as
// secondary detail.

var (
	reURL          = regexp.MustCompile(`"(https?://[^"]+)"`)
	reProxyConnect = regexp.MustCompile(`proxyconnect tcp: dial tcp ([^:]+:\d+)`)
)

// PlainNetError returns a sentence for a network error in the UI
// language, or "" when raw is not one duet knows (the caller then shows
// raw as it is).
func PlainNetError(raw string) string {
	m := reURL.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	u, err := url.Parse(m[1])
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := u.Hostname()
	switch {
	case host == "github.com" || strings.HasSuffix(host, ".github.com") || strings.HasSuffix(host, ".githubusercontent.com"):
		host = "GitHub"
	}
	T := Cur()
	s := T.NetUnreachable(host)
	if p := reProxyConnect.FindStringSubmatch(raw); p != nil {
		s += T.NetViaProxy(p[1])
	}
	if why := netReason(raw); why != "" {
		s += T.NetReasonSep + why
	}
	return s
}

// NetReason is why a connection failed, in the UI language ("" when duet
// does not know the error).
func NetReason(raw string) string { return netReason(raw) }

func netReason(raw string) string {
	T := Cur()
	r := strings.ToLower(raw)
	switch {
	case strings.Contains(r, "proxyconnect") && strings.Contains(r, "connection refused"):
		return T.NetProxyDown
	case strings.Contains(r, "connection refused"):
		return T.NetRefused
	case strings.Contains(r, "deadline exceeded"), strings.Contains(r, "timeout"):
		return T.NetTimeout
	case strings.Contains(r, "no such host"):
		return T.NetNoHost
	case strings.Contains(r, "certificate"), strings.Contains(r, "x509"), strings.Contains(r, "tls:"):
		return T.NetTLS
	case strings.Contains(r, "status 403"), strings.Contains(r, "rate limit"):
		return T.NetRateLimited
	}
	return ""
}
