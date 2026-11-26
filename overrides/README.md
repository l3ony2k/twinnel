# Built-in override templates

Generic templates, no personal data. Personal layers live in the user's data
directory, never in this repo.

| File | Layer | Notes |
|---|---|---|
| `base-tun-fakeip.yaml` | base (managed) | ports, controller, fake-ip DNS, TUN |
| `direct-rules.example.yaml` | user | example of prepending rules |

The Tailscale coexistence layer and the leak guard are generated in code
(`core/config/tailscale.go`, `core/config/leakguard.go`), not stored here.

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
converted with `config.ImportVergeMerge` / `duet compose --verge FILE`.
