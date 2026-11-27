package engine

import (
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/duet/core/rpc"
)

// What a hot reload (PUT /configs) does in mihomo v1.19.34
// (hub/executor.ApplyConfig; details and measurements in
// docs/reload-behavior.md):
//
//   - The TUN listener is re-created only when the parsed tun config differs
//     (listener.ReCreateTun: LC.Tun.Sort then Equal). The list fields below
//     are sorted first, so their order never counts.
//   - The DNS server is re-bound only when dns.listen changes; otherwise only
//     its resolver is swapped (dns.ReCreateServer). The fake-ip pool and
//     mapping are copied into the new resolver (PatchFrom).
//   - Inbound ports and allow-lan/bind-address are applied only with
//     force=true, and a listener is re-created only when its address
//     changed.
//   - Proxy providers other than inline ones close their open connections
//     in Initial(), on every reload.
//   - The external controller (address, secret, ...) is not touched at all.

// tunSortedLists are the tun fields mihomo sorts before comparing
// (listener/config.Tun.Sort).
var tunSortedLists = map[string]bool{
	"dns-hijack": true, "inet4-address": true, "inet6-address": true,
	"route-address": true, "route-address-set": true,
	"route-exclude-address": true, "route-exclude-address-set": true,
	"include-interface": true, "exclude-interface": true,
	"include-uid": true, "include-uid-range": true, "exclude-uid": true, "exclude-uid-range": true,
	"include-android-user": true, "include-package": true, "exclude-package": true,
	"include-mac-address": true, "exclude-mac-address": true,
	"inet4-route-address": true, "inet6-route-address": true,
	"inet4-route-exclude-address": true, "inet6-route-exclude-address": true,
}

// inboundKeys are applied by updateListeners only when force is true.
var inboundKeys = []string{
	"port", "socks-port", "redir-port", "tproxy-port", "mixed-port",
	"allow-lan", "bind-address", "skip-auth-prefixes", "lan-allowed-ips", "lan-disallowed-ips",
	"ss-config", "vmess-config", "tuic-server",
}

// controllerKeys are read only when mihomo starts (hub.applyRoute).
var controllerKeys = []string{
	"external-controller", "external-controller-tls", "external-controller-unix",
	"external-controller-pipe", "external-controller-cors", "external-doh-server", "secret",
}

const defaultFakeIPRange = "198.18.0.1/16"

// ReloadImpact compares the config mihomo runs (old) with the one it will
// load (new). A config that does not parse gives the most careful answer:
// force, TUN and DNS listener assumed to change.
func ReloadImpact(oldCfg, newCfg []byte) rpc.ReloadImpact {
	var a, b map[string]any
	errA := yaml.Unmarshal(oldCfg, &a)
	errB := yaml.Unmarshal(newCfg, &b)
	if errA != nil || errB != nil || a == nil || b == nil {
		im := rpc.ReloadImpact{Force: true, TUNRebuild: true, DNSListener: true, Disruptive: true, Unknown: true}
		im.Notes = append(im.Notes, "could not compare the old and the new config; assuming the worst")
		return im
	}
	return reloadImpactMaps(a, b)
}

func reloadImpactMaps(a, b map[string]any) rpc.ReloadImpact {
	var im rpc.ReloadImpact

	// TUN
	ta, tb := asMap(a["tun"]), asMap(b["tun"])
	if truthy(ta["enable"]) || truthy(tb["enable"]) {
		var why []string
		if !reflect.DeepEqual(normTun(ta), normTun(tb)) {
			why = append(why, "tun section")
			im.TUNWhy = append(im.TUNWhy, "tun")
		}
		if fakeIPAddr(a) != fakeIPAddr(b) {
			why = append(why, "dns.fake-ip-range (the TUN address comes from it)")
			im.TUNWhy = append(im.TUNWhy, "fake-ip-range")
		}
		if len(why) > 0 {
			im.TUNRebuild = true
			im.Notes = append(im.Notes, "the TUN device is closed and created again ("+strings.Join(why, ", ")+" changed): routes are re-added and every connection through the TUN is cut, about a second without network")
		}
	}

	// DNS listener
	da, db := asMap(a["dns"]), asMap(b["dns"])
	ea, eb := truthy(da["enable"]), truthy(db["enable"])
	if ea != eb || (eb && str(da["listen"]) != str(db["listen"])) {
		im.DNSListener = true
		im.Notes = append(im.Notes, fmt.Sprintf("the DNS server is closed and bound again (dns.enable %v→%v, dns.listen %q→%q): queries fail for a moment", ea, eb, str(da["listen"]), str(db["listen"])))
	}

	// inbound listeners (force)
	for _, k := range inboundKeys {
		if !reflect.DeepEqual(norm(a[k]), norm(b[k])) {
			im.Listeners = append(im.Listeners, k)
		}
	}
	if !reflect.DeepEqual(norm(a["listeners"]), norm(b["listeners"])) {
		im.Listeners = append(im.Listeners, "listeners")
	}
	im.Force = len(im.Listeners) > 0
	if len(im.Listeners) > 0 {
		im.Notes = append(im.Notes, "inbound listeners change ("+strings.Join(im.Listeners, ", ")+"): the changed ports are closed and opened again; apps set to an old port lose the proxy")
	}

	// proxy providers close their connections on every reload
	for name, v := range asMap(b["proxy-providers"]) {
		if strings.ToLower(str(asMap(v)["type"])) != "inline" {
			im.ProviderConnsReset = append(im.ProviderConnsReset, name)
		}
	}
	sort.Strings(im.ProviderConnsReset)
	if len(im.ProviderConnsReset) > 0 {
		im.Notes = append(im.Notes, "open connections through proxy providers ("+strings.Join(im.ProviderConnsReset, ", ")+") are closed by mihomo on every reload")
	}

	// selected nodes
	if p := asMap(b["profile"]); p["store-selected"] != nil && !truthy(p["store-selected"]) {
		for _, g := range asList(b["proxy-groups"]) {
			if strings.EqualFold(str(asMap(g)["type"]), "select") {
				im.SelectionReset = true
				im.Notes = append(im.Notes, "profile.store-selected is off: select groups go back to their first entry")
				break
			}
		}
	}

	// controller: not applied by a hot reload
	for _, k := range controllerKeys {
		va, vb := norm(a[k]), norm(b[k])
		if va == "" {
			va = nil
		}
		if vb == "" {
			vb = nil
		}
		if !reflect.DeepEqual(va, vb) {
			im.RestartNeeded = append(im.RestartNeeded, k)
		}
	}
	if len(im.RestartNeeded) > 0 {
		im.Notes = append(im.Notes, "a hot reload does not apply "+strings.Join(im.RestartNeeded, ", ")+"; the controller keeps its address and secret until mihomo restarts")
	}

	im.Disruptive = im.TUNRebuild || im.DNSListener || len(im.Listeners) > 0 || len(im.ProviderConnsReset) > 0
	return im
}

// normTun sorts the lists mihomo sorts and canonicalises prefixes.
func normTun(t map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range t {
		v = norm(v)
		if v == nil {
			continue
		}
		if tunSortedLists[k] {
			if l, ok := v.([]any); ok {
				ss := make([]string, 0, len(l))
				for _, x := range l {
					s := str(x)
					if p, err := netip.ParsePrefix(s); err == nil {
						s = p.String()
					} else if ad, err := netip.ParseAddr(s); err == nil {
						s = netip.PrefixFrom(ad, ad.BitLen()).String()
					}
					ss = append(ss, s)
				}
				slices.Sort(ss)
				v = ss
			}
		}
		out[k] = v
	}
	return out
}

// fakeIPAddr is the address part of dns.fake-ip-range; the TUN's inet4
// address is that address /30 (config.parseTun).
func fakeIPAddr(c map[string]any) string {
	r := str(asMap(c["dns"])["fake-ip-range"])
	if r == "" {
		r = defaultFakeIPRange
	}
	if p, err := netip.ParsePrefix(r); err == nil {
		return p.Addr().String()
	}
	return r
}

// norm makes YAML values comparable: empty lists and maps become nil,
// numbers and booleans keep their type.
func norm(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		if len(x) == 0 {
			return nil
		}
		out := make([]any, len(x))
		for i := range x {
			out[i] = norm(x[i])
		}
		return out
	case map[string]any:
		if len(x) == 0 {
			return nil
		}
		out := make(map[string]any, len(x))
		for k, vv := range x {
			if n := norm(vv); n != nil {
				out[k] = n
			}
		}
		return out
	default:
		return x
	}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func truthy(v any) bool {
	b, _ := v.(bool)
	return b
}
