# duet architecture

This describes the code as it is. Product goals are in `requirements.md`, the original technical plan in `tech.md` (both Chinese).

## Components

```
 user                                         root
┌──────────────────────────────┐            ┌──────────────────────────────────┐
│ duet.app  (cmd/app, MyGo)    │  JSON-RPC  │ duet-helper  (cmd/helper)        │
│ duet CLI  (cmd/cli)          │◀──────────▶│  core/engine: compose, apply,    │
│                              │ unix socket│  subscriptions, layers, history, │
│ read-only, direct:           │            │  tailnet poller, kernels         │
│  mihomo REST API             │            │  core/store: state dir           │
│  Tailscale LocalAPI / CLI    │            │  core/service + platform/*:      │
│  health checks (core/health) │            │  service definitions and plans   │
└──────────────────────────────┘            └──────────────────────────────────┘
            │                                          │ writes config, PUT /configs
            ▼                                          ▼
     mihomo (official binary, system service)    tailscaled (official, system service)
```

| Path | Role |
|---|---|
| `cmd/app` | Menu bar app (MyGo native UI): tray icon, flyout panel, main window. Reads mihomo and Tailscale directly and runs the health checks in process. No business logic. |
| `cmd/cli` | `duet`: local commands (`doctor`, `takeover`, `compose`, `status`) and the helper client commands. |
| `cmd/helper` | The root daemon. Thin: wires `core/engine` to `core/rpc` and the platform service manager. |
| `core/config` | YAML node-level merge, Clash Verge import, compose, Tailscale coexist generator, leak guard, `mihomo -t` validation, semantic diff. |
| `core/engine` | The helper's logic, platform-free: apply pipeline, subscriptions, layers, settings, tailnet refresh, kernels, status. |
| `core/rpc` | JSON-RPC 2.0 server/client, method allowlist, peer credential check, event bus. |
| `core/store` | The helper's state directory. |
| `core/kernel` | mihomo release check, verified download, atomic switch and rollback. |
| `core/service` | Platform-neutral service `Spec`, `Manager` interface, `Plan` of explicit steps. |
| `core/health` | Health check framework and the checks. |
| `core/takeover` | Discovery of a hand-made setup and the takeover plan. |
| `core/status`, `core/mihomo`, `core/tailscale`, `core/i18n` | App-side status model and alerts, mihomo REST client, Tailscale LocalAPI client and install detection, UI strings. |
| `platform/common` | Parsers shared by platforms (`tailscale status --json`, mihomo API and config file). |
| `platform/darwin` | macOS facts for the health checks, network change watcher, egress test; `launchd/` service manager. |
| `platform/linux` | Linux facts for the health checks, takeover probe; `systemd/` service manager. |
| `overrides/` | Generic built-in layer templates. |

`core/...` never imports `platform/...`; platforms implement interfaces defined in core (`health.Env`, `service.Manager`, `takeover.Probe`).

## Helper RPC

- Transport: a Unix socket (`/var/run/duet/helper.sock` by default, `$DUET_SOCKET` to override), one JSON-RPC 2.0 object per line, messages up to 16 MiB. Requests on one connection run concurrently (up to 8) and are matched by id.
- Access: the socket is mode 0660 and owned by a group (default `admin`). Each connection's peer uid comes from the kernel (`LOCAL_PEERCRED` on macOS, `SO_PEERCRED` on Linux). Allowed: root, the helper's own uid, and members of the group.
- Methods are a fixed allowlist (`core/rpc/api.go`); registering any other name panics, and unknown names get error -32601. Params are decoded with unknown fields rejected.

| Group | Methods |
|---|---|
| status | `status.get` |
| compose | `compose.preview`, `compose.apply` |
| history | `history.list`, `history.rollback` |
| layers | `layer.list`, `layer.get`, `layer.put`, `layer.delete`, `layer.set_enabled`, `layer.reorder` |
| subscriptions | `subscription.list`, `subscription.put`, `subscription.delete`, `subscription.activate`, `subscription.refresh` |
| settings | `settings.get`, `settings.update` (only leak-guard groups, coexist on/off, tailnet resolver, history size, kernel channel, auto-apply) |
| tailnet | `tailnet.refresh` |
| kernels | `kernel.list`, `kernel.check`, `kernel.install` (takes a channel, never a URL), `kernel.rollback` |
| info | `tailscale.info`, `service.status` |
| events | `events.wait {after, timeout_ms}`: long-poll on a ring buffer. Types: `compose.applied/failed`, `state.changed`, `subscription.updated/failed`, `tailnet.changed`, `kernel.changed`, `check.changed`, `service.changed`, `helper.started/stopping`, `events.truncated`. |

No method takes a filesystem path or a command. Paths (mihomo binary, config dir and file, controller address, service labels) live only in the helper's `settings.json`. There are no service mutations over RPC yet.

## Apply pipeline

`core/engine/apply.go`, used by `compose.apply`, subscription refresh and tailnet changes:

1. **Compose** the active subscription with the enabled layers. Layers are sorted by stage: `base` → `user` (in saved order) → `coexist` (Tailscale, generated) → `guard` (leak guard, generated), so the guard always runs last and the Tailscale rules end up at the very top.
2. **Strip** the top-level `secret`. Hashes and history use this form.
3. **Validate** with the official `mihomo -t` in a temp dir (geo files copied in, the real config dir allowed as a safe path). If it fails, nothing is written.
4. Unchanged bytes are a no-op unless forced. If mihomo is not running, the file is written and used at the next start (with a warning).
5. **Back up** the file on disk if it is not already the latest history entry (its `secret:` line removed).
6. **Write** atomically, re-inserting the controller secret (from the secret store, a layer, or the file being replaced), keeping the file mode.
7. **Reload** with `PUT /configs?force=true`, then poll `GET /version` for up to 10 s.
8. On a reload or verify failure, **restore** the old bytes and reload them; the result says `rolled_back`.
9. Record history (`applied` or `rollback`) and publish an event.

Subscriptions are fetched through mihomo's mixed port when the controller answers, else directly, with User-Agent `clash.meta`. Content without `proxies`/`proxy-providers` is refused. The URL never appears in errors or logs.

## Override syntax

Layers use the Clash Party (Mihomo Party) merge syntax on YAML nodes, keeping key order and comments:

| Key form | Effect |
|---|---|
| `key: {...}` | deep-merge the mapping |
| `key!: ...` | replace the value |
| `key: [...]` | replace the list |
| `+key: [...]` | prepend to the list |
| `key+: [...]` | append to the list |
| `'<+.lan>': ...` | a literal key that contains `+` or `!` |

Clash Verge Rev merge files (`prepend-rules`, `append-proxies`, ...) are converted on import. After merging, exact duplicates in `rules`, `dns.fake-ip-filter` and `tun.route-exclude/include-address` are removed, and the data sections (`proxies`, `proxy-groups`, `rules`, providers) move to the end. See `overrides/README.md`.

## The Tailscale coexist layer

Generated from `tailscale status` by `config.TailscaleCoexist` and regenerated by the helper when the tailnet changes (peers, IPs, or the Tailscale interface name):

- **Routing.** `tun.route-exclude-address+` gets the tailnet ranges (`100.64.0.0/10`, `fd7a:115c:a1e0::/48`), so mihomo's TUN never captures tailnet traffic.
- **Rules**, prepended: `PROCESS-NAME,tailscaled`, `DOMAIN-SUFFIX` for the Tailscale control and DERP domains and the MagicDNS suffix, `DOMAIN,<peer>` for each peer, and the tailnet CIDRs with `no-resolve`, all `DIRECT`. tailscaled's own traffic (control plane, DERP, WireGuard UDP) must never go through a proxy.
- **DNS.** With fake-ip, tailnet names must get real answers from Tailscale's resolver `100.100.100.100`:
  - `dns.fake-ip-filter+`: the Tailscale domains and `'*'` (mihomo's one-label wildcard, so bare short names like `nas` are never fake).
  - `dns.nameserver-policy`: the MagicDNS suffix, `'*'`, the `100.in-addr.arpa` reverse zones of the tailnet range and the IPv6 reverse zone go to quad-100. Other tailnets' public `*.ts.net` names keep using the normal upstream.
  - `hosts` (with `dns.use-hosts: true`): every peer's short name and FQDN → its tailnet IPs, so forward lookups need no round-trip at all.

  **Why `#<iface>` on the resolver.** With `tun.auto-detect-interface` or `interface-name`, mihomo binds its own sockets to the physical interface. A query to `100.100.100.100` then leaves through Wi-Fi/Ethernet and never reaches tailscaled, so tailnet names time out. Pinning the resolver as `100.100.100.100#<tailscale interface>` fixes that; the hosts entries keep forward lookups working even if the interface name is stale. Pointing tailnet names at `system` instead makes mihomo ask the system resolver, which is mihomo itself, and the query loops until it fails. The `tailnet-resolver-binding` and `magicdns` checks detect both problems.

## Health checks

A check implements `ID`, `Title`, `Platforms`, `Run(ctx, Env) Result`. `Env` is the read-only view of the machine, implemented per platform and faked in tests; `Memo` makes one report fetch each fact once. Results are `ok`, `warn`, `fail` or `skip`, with evidence lines and an optional fix hint. Reports export as text, Markdown and JSON.

| ID | What it checks |
|---|---|
| `tailnet-route` | Quad-100, a few peers and the IPv6 tailnet address route to Tailscale's interface, not to mihomo's TUN. |
| `tun-route-exclude` | mihomo's `route-exclude-address` covers the tailnet ranges. |
| `tailscale-direct-rules` | Evaluates the rules (first match) for tailscaled's process, control and DERP domains and tailnet IPs; each must hit `DIRECT`. |
| `magicdns` | Self and peer names resolve to tailnet IPs (not fake-ips) through the OS resolver (fail) and through mihomo's DNS (warn); the OS has a resolver for the tailnet suffix. |
| `peer-short-names` | Short names in the rules match the current peers. |
| `tailnet-resolver-binding` | When mihomo binds to the physical interface, the tailnet resolver is pinned to the current Tailscale interface or peers are in `hosts`. |
| `fake-ip-filter-mode` | `'*'` in `fake-ip-filter` is only used with the blacklist mode. |
| `system-dns` | Every active network service / link resolves through mihomo, or its queries are caught by the TUN's DNS hijack (warn). |
| `vpn-conflict` | No other VPN is connected, no exit node (unless allowed), and mihomo's TUN holds the default route. |
| `peer-direct` | Active (or chosen) peers are connected directly, not through DERP. |
| `leak-guard` | Sensitive proxy groups (default `Google`, `Meta`) cannot resolve to `DIRECT`. |
| `leak-guard-egress` | Opt-in. One HTTPS request direct and one through mihomo; the proxied exit IP must differ. Public IPs are masked. |
| `controller-security` | The mihomo controller listens on loopback only and has a secret. |

All checks are marked for every platform; what differs is the `Env`:

| Fact | macOS | Linux |
|---|---|---|
| route lookup / table | `route -n get`, `netstat -rn` | `ip -j route get`, `ip -j route show table all` (Tailscale's table 52 included) |
| interfaces | `ifconfig` | `ip -j addr` |
| resolvers | `scutil --dns` | `/etc/resolv.conf`; with the systemd-resolved stub, `resolvectl status` (per-link servers and routing domains) |
| network services | `networksetup` service order and DNS | physical links (not loopback, not tun devices) ordered by default-route metric, named after the active NetworkManager connection when `nmcli` exists |
| VPNs | `scutil --nc list` | NetworkManager `vpn` and `wireguard` connections; none without NetworkManager |
| OS resolver lookup | `dscacheutil` | `getent ahostsv4` (NSS) |
| egress test | socket bound with `IP_BOUND_IF` | not implemented (the check skips) |

Linux caveats: `system-dns` labels per-link servers as manual even when they came from DHCP, and its fix hint names the macOS command. `vpn-conflict` fails hard only for another `utun*` device holding the default route (a macOS name); on Linux another tun device gives a warning.

`core/health` also has a `Scheduler` (interval plus debounced triggers such as the macOS `NetWatcher`), meant for the helper but not wired into it yet. Today the app runs the checks in process on its own timer, and `duet doctor` runs them once.

## Takeover

`duet takeover plan` turns a hand-made setup into duet's model without changing anything. It finds the mihomo and Tailscale services (launchd plists on macOS, systemd units on Linux), reads the live config, scores candidate subscription files next to it, and derives layers: a `base` layer with the settings that differ from the subscription, `user` layers for hand-added rules, and the coexist layer in place of the hand-written Tailscale parts. It then composes them, runs `mihomo -t` with the service's own binary, and compares the result with the live config using a semantic diff (settings as sets where order does not matter, proxies by content hash, rules under first-match semantics). The output directory holds the layers, the composed config, the diff and a `plan.json` without credentials. There is no apply step yet.

## Services

`core/service.Manager` renders a definition and turns install / uninstall / start / stop / restart into a `Plan` of explicit steps (mkdir, write, copy, remove, run), which can be printed (`duet helper install --dry-run`) or applied with an executor; tests apply plans with fakes.

| | macOS (`platform/darwin/launchd`) | Linux (`platform/linux/systemd`) |
|---|---|---|
| definition | `/Library/LaunchDaemons/<label>.plist` | `/etc/systemd/system/<label>.service` |
| install | `bootout` (ignored), write, `bootstrap system`, `enable` | write, `daemon-reload`, `enable`, `restart` |
| stop | `bootout` (KeepAlive would restart a killed process) | `stop` |
| status | `launchctl print system/<label>` | `systemctl show <unit> --property=...` |
| keep alive | `KeepAlive`, `ThrottleInterval` | `Restart=always`, `RestartSec` |

The helper uses launchd on macOS. On Linux the systemd manager exists and is tested, but `cmd/helper` does not use it yet, and `duet helper install` is macOS-only.

## Data locations

| What | Where |
|---|---|
| Helper state (settings, subscriptions, layers, history, kernels, tailnet cache, secrets file) | `/Library/Application Support/duet` on macOS (`--state-dir`). Files 0600, directories 0700. |
| Helper socket | `/var/run/duet/helper.sock` |
| Helper binary and service (planned install) | `/Library/PrivilegedHelperTools/com.l3ony2k.duet.helper`, LaunchDaemon `com.l3ony2k.duet.helper` |
| Managed mihomo kernels | `<state>/kernels/`, with `current` and `previous` symlinks |
| App preferences | `<user config dir>/duet/app.json` |
| Takeover plans | a new temp dir, or `--out` |

## Security model

- **One privileged process.** Only the helper runs as root. The app and CLI read mihomo and Tailscale directly, and everything that writes goes through the helper.
- **Narrow RPC.** Fixed method allowlist, strict param decoding, peer uid check, no paths or commands from clients. Kernel installs take only a channel; the helper picks the asset and refuses one without a published SHA-256 digest.
- **Validate before write, back up before write, roll back on failure.** The running config is never replaced by one `mihomo -t` rejects.
- **Secrets.** The controller secret is kept out of history and backups and injected only into the live file. Subscription URLs are redacted in errors, logs, takeover plans and diffs. The config parser used by health checks never reads proxies or providers. The secret store is a 0600 file today; a system keychain store is planned.
- **Read-only diagnostics.** Health checks and takeover only read files, run read-only commands and send GET requests to the controller.
- **No personal data in the repo.** Test data is synthetic; `scripts/check-no-personal-data.sh` greps tracked files for patterns kept outside the tree.
