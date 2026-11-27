//go:build darwin

package darwin

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ParseScutilProxy reads `scutil --proxy` and returns the proxy an HTTPS
// request goes through: the HTTPS proxy, else the SOCKS one, else the HTTP
// one; nil when none is on. Hosts in ExceptionsList are not handled (the
// one caller asks api.github.com).
func ParseScutilProxy(out string) *url.URL {
	kv := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), " : ")
		if ok {
			kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	for _, p := range []struct{ key, scheme string }{{"HTTPS", "http"}, {"SOCKS", "socks5"}, {"HTTP", "http"}} {
		if kv[p.key+"Enable"] != "1" || kv[p.key+"Proxy"] == "" {
			continue
		}
		host := kv[p.key+"Proxy"]
		if port := kv[p.key+"Port"]; port != "" && port != "0" {
			host = net.JoinHostPort(host, port)
		}
		return &url.URL{Scheme: p.scheme, Host: host}
	}
	return nil
}

// SystemProxy is an http.Transport Proxy function that follows the
// system's proxy settings (read once per request with `scutil --proxy`),
// then the environment (HTTPS_PROXY ...).
func SystemProxy(run CommandRunner) func(*http.Request) (*url.URL, error) {
	if run == nil {
		run = ExecRunner(3 * time.Second)
	}
	return func(r *http.Request) (*url.URL, error) {
		if out, err := run(context.Background(), "/usr/sbin/scutil", "--proxy"); err == nil {
			if u := ParseScutilProxy(string(out)); u != nil {
				return u, nil
			}
		}
		return http.ProxyFromEnvironment(r)
	}
}
