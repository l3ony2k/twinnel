package engine

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/l3ony2k/duet/core/store"
)

// exposureCheck looks at the controller settings of a config about to be
// written. The subscription is written by a third party and is the base of
// every composed config, so it can set any top-level key; mihomo runs as
// root. Refused (errors):
//
//   - external-controller (or -tls) listening beyond loopback without a
//     secret: anyone on the network could reconfigure the proxy;
//   - external-controller-unix / -pipe: an unauthenticated controller on a
//     socket mihomo (root) creates at a path the config chooses; allowed
//     only inside the config dir.
//
// Warned: a non-loopback controller with a secret.
func exposureCheck(final []byte, st *store.Settings) (warnings []string, errs []string) {
	secret, _ := topLevel(final, "secret")
	for _, key := range []string{"external-controller", "external-controller-tls"} {
		addr, ok := topLevel(final, key)
		if !ok || strings.TrimSpace(addr) == "" {
			continue
		}
		if loopbackAddr(addr) {
			continue
		}
		if secret == "" {
			errs = append(errs, fmt.Sprintf("duet refuses %s: %s listens beyond this Mac and the controller has no secret (anyone on the network could reconfigure the proxy); bind it to 127.0.0.1 or run controller.secure", key, addr))
		} else {
			warnings = append(warnings, fmt.Sprintf("%s: %s is reachable from the network (protected by the secret only)", key, addr))
		}
	}
	for _, key := range []string{"external-controller-unix", "external-controller-pipe"} {
		p, ok := topLevel(final, key)
		if !ok || strings.TrimSpace(p) == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(st.Mihomo.ConfigDir, p)
		}
		if !inside(st.Mihomo.ConfigDir, filepath.Clean(p)) {
			errs = append(errs, fmt.Sprintf("duet refuses %s: %s is outside the config dir %s (mihomo, as root, would create an unauthenticated controller socket there)", key, p, st.Mihomo.ConfigDir))
		}
	}
	return warnings, errs
}

// loopbackAddr reports whether a host:port only listens on loopback. An
// empty host (":9090") or 0.0.0.0 listens everywhere.
func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
