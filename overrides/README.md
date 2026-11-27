# Built-in override templates

Generic templates, no personal data. Personal layers live in the user's data
directory, never in this repo.

| File | Layer | Notes |
|---|---|---|
| `base-tun-fakeip.yaml` | base (managed) | ports, controller, fake-ip DNS, TUN |
| `direct-rules.example.yaml` | user | example of prepending rules |

The Tailscale coexistence layer and the leak guard are generated in code
(`core/config/tailscale.go`, `core/config/leakguard.go`), not stored here.

## Tailscale coexist and MagicDNS

With MagicDNS on, tailnet names (the MagicDNS suffix, single-label names,
the tailnet reverse zones) go to Tailscale's resolver `100.100.100.100`
through `dns.nameserver-policy`.

With MagicDNS off (or no MagicDNS suffix in `tailscale status`), that
resolver is not authoritative for those names: it answers only full node
names and forwards everything else to its upstream, which here is the
system resolver, that is mihomo. So no `nameserver-policy` entry is written.
Peer names (`nas` and `nas.<tailnet>.ts.net`) still resolve from `hosts`,
and the tailnet CIDR rules and `route-exclude-address` stay.

## Leak guard and nested groups

The leak guard changes only the groups you name. It removes DIRECT and
`type: direct` proxies from them and sets `empty-fallback: REJECT`.
Without that, mihomo uses a direct dialer (`COMPATIBLE`) when a group has
no members at run time.

Nested groups (groups listed inside a guarded group) are not changed by
default, because the same group can also carry traffic that should go
DIRECT. The trade-off: a guarded group can still leak through a nested
group that lists DIRECT, or through one that is empty at run time (a failed
provider, a filter that matches nothing). The leak guard warns about both.

The generator option `leak_guard_nested: true` (`twinnel compose
--leak-guard-nested`) also sets `empty-fallback: REJECT` on nested groups
that only guarded groups use. A nested group counts as shared, and is left
alone, when an unguarded group lists it or when a rule, a sub-rule, a
`dialer-proxy` or a DNS `server#group` entry names it. DIRECT members of
nested groups are never removed; move DIRECT out of such a group yourself.

## Syntax (Clash Party / Mihomo Party compatible)

| Key form | Effect |
|---|---|
| `key: {...}` | deep-merge the mapping |
| `key!: ...` | replace the value wholesale |
| `key: [...]` | replace the list |
| `+key: [...]` | prepend to the list |
| `key+: [...]` | append to the list |
| `'<+.lan>': ...` | literal key containing `+`/`!` (markers go outside: `'<a+>+'`) |

Clash Verge Rev "Merge" files (`prepend-rules`, `append-proxies`, ...) are
converted with `config.ImportVergeMerge` / `twinnel compose --verge FILE`.
