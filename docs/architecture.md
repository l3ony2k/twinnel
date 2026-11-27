# Twinnel architecture

This describes the code as it is. Product goals are in `requirements.md`, the original technical plan in `tech.md` (both Chinese).

## Components

```
 user                                         root
┌──────────────────────────────┐            ┌──────────────────────────────────┐
│ Twinnel.app  (cmd/app, MyGo)    │  JSON-RPC  │ twinnel-helper  (cmd/helper)        │
│ Twinnel CLI  (cmd/cli)          │◀──────────▶│  core/engine: compose, apply,    │
│                              │ unix socket│  subscriptions, layers, history, │
│ read-only, direct:           │            │  tailnet poller, kernels, geo/UI │
│  mihomo REST API             │            │  health scheduler (core/health)  │
│  Tailscale LocalAPI / CLI    │            │  core/store: state dir, secrets  │
│  health checks (core/health) │            │  core/service + platform/*:      │
│                              │            │  service definitions and plans   │
└──────────────────────────────┘            └──────────────────────────────────┘
            │                                          │ writes config, PUT /configs
            ▼                                          ▼
     mihomo (official binary, system service)    tailscaled (official, system service)
```

| Path | Role |
|---|---|
| `cmd/app` | Menu bar app (MyGo native UI): tray icon, flyout panel, main window. Reads mihomo and Tailscale directly and runs the health checks in process; the subscriptions and overrides, history and kernel pages, and 重载配置 when the helper is connected, go through the helper. No business logic. |
| `cmd/cli` | `twinnel`: local commands (`doctor`, `takeover plan`, `compose`, `status`) and the helper client commands (including `helper install`, `helper adopt`, `helper secure-controller`, `helper harden`, `import`, `export`). Every command reaches the helper and mihomo through `cmd/cli/helperconn.go` (`dialHelper`, `controllerFor`, `healthSettings`): never its own dial, app.json read or secret. |
| `cmd/helper` | The root daemon. Thin: wires `core/engine` to `core/rpc`, the secret store, the platform service manager, the Tailscale interface lookup and the health scheduler (`platform_darwin.go`, `platform_linux.go`). |
| `core/config` | YAML node-level merge, Clash Verge import, compose, Tailscale coexist generator, leak guard, `mihomo -t` validation, semantic diff. |
| `core/engine` | The helper's logic, platform-free: apply pipeline, subscriptions, layers, settings, tailnet refresh, kernels, status, takeover plans and adopt, controller secret, harden. |
| `core/rpc` | JSON-RPC 2.0 server/client, method allowlist, peer credential check, event bus. `core/rpc/healthsrc`: the helper's health reports as a `status.HealthSource`. |
| `core/store` | The helper's state directory and the `Secrets` interface (file store; migration into another store). |
| `core/kernel` | mihomo release check, verified download, atomic switch and rollback. |
| `core/service` | Platform-neutral service `Spec`, `Manager` interface, `Plan` of explicit steps. |
| `core/health` | Health check framework and the checks. |
| `core/takeover` | Discovery of a hand-made setup and the takeover plan. |
| `core/setup` | The fresh-setup flow (app 设置这台 Mac, `twinnel setup`): idempotent steps over the helper RPC and the Tailscale LocalAPI. |
| `core/sysdns` | Point the system DNS at mihomo with a backup, and restore it. |
| `core/status`, `core/mihomo`, `core/tailscale`, `core/i18n` | App-side status model and alerts, mihomo REST client, Tailscale LocalAPI client and install detection, UI strings. |
| `platform/common` | Parsers shared by platforms (`tailscale status --json`, mihomo API and config file). |
| `platform/darwin` | macOS facts for the health checks, network change watcher, egress test; `launchd/` service manager; `keychain/` secret store. |
| `platform/linux` | Linux facts for the health checks, takeover probe, install locations and the helper install plan; `systemd/` service manager. |
| `overrides/` | Generic built-in layer templates. |

`core/...` never imports `platform/...`; platforms implement interfaces defined in core (`health.Env`, `service.Manager`, `takeover.Probe`).

## Helper RPC

- Transport: a Unix socket (`/var/run/twinnel/helper.sock` by default, `$TWINNEL_SOCKET` to override), one JSON-RPC 2.0 object per line, messages up to 16 MiB. Requests on one connection run concurrently (up to 8) and are matched by id.
- Access: the socket is mode 0660 and owned by a group (`--group`; `admin` on macOS; on Linux `admin` only if it exists, else `sudo`, else `wheel`, else `twinnel`, see [Linux](#linux)). Each connection's peer credentials come from the kernel (`LOCAL_PEERCRED` on macOS with all groups; `SO_PEERCRED` on Linux, which carries only the primary group). Allowed: root, the helper's own uid, and members of the group. On Linux a user whose *primary* group is not the socket group is refused today, even when they are a member.
- Methods are a fixed allowlist (`core/rpc/api.go`); registering any other name panics, and unknown names get error -32601. Params are decoded with unknown fields rejected.

| Group | Methods |
|---|---|
| status | `status.get` |
| compose | `compose.preview`, `compose.apply`, `compose.status` (hash and whether the composed config differs from the file on disk, without `mihomo -t`: the app's 未应用 check after every edit) |
| history | `history.list`, `history.rollback` |
| layers | `layer.list`, `layer.get`, `layer.put`, `layer.delete`, `layer.set_enabled`, `layer.reorder` |
| subscriptions | `subscription.list`, `subscription.put`, `subscription.delete`, `subscription.activate`, `subscription.refresh` |
| settings | `settings.get`, `settings.update` (only leak-guard groups, coexist on/off, tailnet resolver, history size, kernel channel, auto-apply) |
| tailnet | `tailnet.refresh` |
| kernels | `kernel.list`, `kernel.check`, `kernel.install` (takes a channel, optionally the version that was shown and a download `proxy`: `direct`, `subscription` or a proxy URL; never a download URL), `kernel.rollback` |
| accessories | `geo.status`, `ui.status` (disk and the last check only, no network), `geo.check {proxy}`, `ui.check {proxy}`, `geo.update {force, proxy, reload}`, `ui.update {force, proxy}`; progress as `accessory.progress` events (download bytes → install → verify → reload); record of what Twinnel installed and the last check in `<state>/accessories.json`; `settings.update {geo_auto_update: weekly\|off}` (default off) |
| services | `service.status`, `service.start`, `service.stop`, `service.restart` (`mihomo`, `tailscaled`), `service.install`, `service.uninstall` (`mihomo` only). The client passes a name; labels come from settings. The helper's own service is refused (use `twinnel helper install/uninstall`). Without root the helper returns the plan with `dry_run: true` and runs nothing. `service.log {name: mihomo|helper, lines, since}`: the tail of a log file under `<state>/logs`, URLs redacted, lines before `since` dropped. `mihomo`: its stdout/stderr files (startup errors the controller's stream never carries); 日志 → mihomo shows them before the live stream; MCP `logs source=mihomo-output`. `helper`: `helper.log`, which only administrators can open on Windows and Linux; 日志 → 后台助手, `twinnel service log helper [--since 10m]`, MCP `logs source=helper`. The helper rotates them at 10 MB by copy and truncate (mihomo's on macOS/Linux, where launchd/systemd write them; `twinnel-svchost` rotates mihomo's on Windows; its own log on every platform). | |
| health | `health.last` (the last report, no run), `health.run {reason}` (runs now, 2 min cap), `health.egress {lang}` (runs `leak-guard-egress` once now and keeps the result, see below) |
| config | `config.get {source: live\|preview\|history, id}`: the config text with the top-level `secret` stripped |
| info | `tailscale.info` |
| setup | `tailscale.login {start}` (login state; with `start`, connect and start an interactive login as root — clients only open the AuthURL), `tailscale.install {proxy}` (the open-source tailscaled and its service, unless any Tailscale is installed), `dns.status` (`pointed` / `unprotected` judge connected services only: `sysdns.LinkConnected` / `sysdns.Counted`, the same rule as the system-dns check; every service carries `connected`), `dns.point` (every managed network service, connected or not, to 127.0.0.1, refused while mihomo's DNS does not answer; old values in `<state>/dns-backup.json`), `dns.restore`. Driven by `core/setup` (`docs/new-machine.md`) |
| controller | `controller.credentials` (the controller address and the secret mihomo loaded, for the app; any authorized peer can already reconfigure mihomo), `controller.secure` (generate a secret, store it, write it into the config and reload; it takes effect at mihomo's next start) |
| takeover | `takeover.plan` (discover, plan and keep the plan under `<state>/takeover/<id>/`), `takeover.plans`, `takeover.discard`, `takeover.adopt {plan_id, confirm_hash}`, `takeover.undo {dry_run}` (write back the pre-takeover copy, remove what adopt imported) (see [Takeover](#takeover)) |
| harden | `service.harden.plan`, `service.harden.plans`, `service.harden.apply`: move an adopted mihomo to the managed kernel, a root-owned config dir and Twinnel's LaunchDaemon in one restart (macOS; see [Takeover](#takeover)) |
| events | `events.wait {after, timeout_ms}`: long-poll on a ring buffer. Types: `compose.applied/failed`, `state.changed`, `subscription.updated/failed`, `tailnet.changed`, `kernel.changed`, `check.changed`, `health.updated`, `accessory.changed`, `service.changed`, `takeover.changed`, `helper.started/stopping`, `events.truncated`, `compose.stage` (an apply's or rollback's progress: `validate`, `write`, `reload`, `verify`; clients show it and keep it out of logs). |

No method takes a filesystem path or a command. Paths (mihomo binary, config dir and file, controller address, service labels) live only in the helper's `settings.json`. Error codes besides JSON-RPC's own: -32001 unauthorized, -32002 failed, -32003 not found, -32004 busy, -32005 not implemented.

## Apply pipeline

`core/engine/apply.go`, used by `compose.apply`, subscription refresh and tailnet changes:

1. **Compose** the active subscription with the enabled layers. Layers are sorted by stage: `base` → `user` (in saved order) → `coexist` (Tailscale, generated) → `guard` (leak guard, generated), so the guard always runs last and the Tailscale rules end up at the very top.
2. **Strip** the top-level `secret`. Hashes and history use this form.
3. **Validate** with the official `mihomo -t` in a temp dir next to `<state>/validate/geo`, the real config dir allowed as a safe path. The config dir's geo files are copied once into that cache (again when they change) and hard-linked into each run; geo files a passing run had to download are kept there too. A passing result is remembered by the bytes, the binary and the geo files, so a preview followed by an apply runs `mihomo -t` once. If it fails, nothing is written.
4. Unchanged bytes are a no-op unless forced. If mihomo is not running, the file is written and used at the next start (with a warning).
5. **Back up** the file on disk if it is not already the latest history entry (its `secret:` line removed).
6. **Write** atomically, re-inserting the controller secret (from the secret store, a layer, or the file being replaced), keeping the file mode. When there is no secret anywhere and the helper manages the config (no file yet, or the file lives in the state dir), it generates one into the secret store and says so in a warning.
7. **Reload** with `PUT /configs`, then poll `GET /version` for up to 10 s. `force=true` only when inbound listeners change (or there was no file to compare); `ReloadImpact` (`core/engine/reloadimpact.go`) also warns when the reload rebuilds the TUN or when a setting (controller address, secret) needs a restart. See `docs/reload-behavior.md`.
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
| `tun-dataplane` | With mihomo's TUN on, one real TCP handshake to an address routed into the TUN (`1.1.1.1:443`, else a fake-ip), 2.5 s. No answer fails, with ICMP-to-gateway and proxy-port evidence; the fix switches `tun.stack` to gvisor (`docs/tun-dataplane.md`). |
| `leak-guard` | Sensitive proxy groups (default `Google`, `Meta`) cannot resolve to `DIRECT`. |
| `leak-guard-egress` | Opt-in. One HTTPS request direct and one through mihomo; the proxied exit IP must differ. Public IPs are masked. Each path tries the IP-echo endpoints in order until one answers (`health.DefaultEgressURLs`: Cloudflare trace on `1.1.1.1`, the same on `www.cloudflare.com`, then two mainland-China echoes; replaced by `health_egress_urls` / `twinnel settings --egress-urls` / `doctor --egress-url`), at most 3.5 s each, 7 s direct and 6 s proxied in all; the evidence names the endpoint that answered. The direct request is pinned to the physical interface (`platform/egress`, one implementation; only the socket option is per OS) and resolves a hostname over that interface too, never with the system resolver (mihomo's fake-ip): first the servers from before Twinnel pointed the system DNS (the helper's `dns-backup.json`), then the interface's DHCP servers, its current ones, its gateway, then AliDNS, DNSPod, Cloudflare, Google (loopback, fake-ip and MagicDNS addresses dropped). With `health_egress` (定期检测) on it runs with every health run; off, it runs only on `health.egress` (检测出口), whose result (with its time, `CheckReport.at`) the helper keeps in `health-egress.json` and puts into later reports for 24 h. |
| `controller-security` | The mihomo controller listens on loopback only and has a secret. |
| `controller-owner` | The mihomo on the controller address is the one Twinnel runs (the helper's preflight, `core/foreign`), not another program's (Clash Party, Clash Verge Rev, ClashX Meta, FlClash or any other mihomo). Fails with `controller-owner/other-client` (quit that app: manual) and `controller-owner/port-shared` (`controller.move`: one click). |

**Which controller the evidence came from.** The helper wraps every run's `Env` with the controller owner (`health.WithOwner`). Every check that read the running mihomo (its controller, its DNS listener, its proxy port) carries `CheckReport.controller`, e.g. "127.0.0.1:9090（Twinnel）". When another program's mihomo holds the address, those checks are `skip` with "… 被 Clash Party 占用，读到的是它的配置，这项不判断" instead of judging that program's rules and TUN; what they would have said stays in the evidence.

**The foreign-controller preflight** (`core/engine/foreign.go`). Before writing and reloading, `install` asks who listens on the controller port (`Engine.PortOwner`: `lsof`+`ps` on macOS, `/proc` on Linux, `GetExtendedTcpTable`+`QueryFullProcessImageName` on Windows) and compares it with the managed or adopted binary, the kernels dir and the mihomo service's PID (and, on Windows, its wrapper's children). Foreign: nothing is written or reloaded and the apply fails with `controller-foreign(-app)`; `status.get` carries `mihomo.owner`; auto-apply (subscription, tailnet) is held back and the newest one runs once the address is no longer foreign. A reload refused with mihomo's `SAFE_PATHS` error marks the controller foreign too (its allowed path names the client), the previous file goes back without another reload, and the raw text stays in `ApplyResult.foreign.detail`, never in the message. Development stacks with a fake controller run the helper with `--no-controller-owner`.

Check titles name the subject, not the passing condition (`vpn-conflict` is "VPN 冲突" / "VPN conflicts"), so a red mark next to a title reads as a problem with that subject. The texts are in `core/health/strings.go` (zh, reference) and `strings_en.go`; zh uses the UI glossary terms (出口节点, 密钥, 后台助手), and a one-sentence fix hint has no final period. `TestChineseUsesGlossaryTerms` and `TestTitlesNameTheSubject` check both.

There is no check for direct vs DERP-relayed peers: relaying usually comes from the network or NAT and cannot be fixed from Twinnel, so it would be a warning nobody can clear. The Tailscale devices page shows the connection path per device instead. Likewise, outcomes that only depend on the network (no active network service, the egress echo unreachable on the direct path) are `skip`, not `warn`.

All checks are marked for every platform; what differs is the `Env`:

| Fact | macOS | Linux |
|---|---|---|
| route lookup / table | `route -n get`, `netstat -rn` | `ip -j route get`, `ip -j route show table all` (Tailscale's table 52 included) |
| interfaces | `ifconfig` | `ip -j addr` |
| resolvers | `scutil --dns` | `/etc/resolv.conf`; with the systemd-resolved stub, `resolvectl status` (per-link servers and routing domains) |
| network services | `networksetup` service order and DNS | physical links (not loopback, not tun devices) ordered by default-route metric, named after the active NetworkManager connection when `nmcli` exists |
| VPNs | `scutil --nc list` | NetworkManager `vpn` and `wireguard` connections; none without NetworkManager |
| OS resolver lookup | `dscacheutil` | `getent ahostsv4` (NSS) |
| egress test | socket bound with `IP_BOUND_IF`; DHCP DNS from `ipconfig getpacket <dev>` | socket bound with `SO_BINDTODEVICE`; DNS from the link's servers, `/run/systemd/resolve/resolv.conf`, the default gateway |

Linux differences, in the checks themselves:
- `system-dns` labels each link's servers by origin. With NetworkManager it reads the connection's `ipv4.method`, `ipv4.dns`, `ipv4.ignore-auto-dns`, `ipv6.dns`, `ipv6.ignore-auto-dns`: 手动 (static only), 手动 + DHCP, DHCP/默认, or 系统全局 DNS when the link has no servers of its own. Without NetworkManager, per-link servers from resolved are labelled as of unknown origin. The fix hint names `nmcli connection modify … ipv4.dns … ipv4.ignore-auto-dns yes`, or `resolved.conf` (`DNS=`, `Domains=~.`) when there is only systemd-resolved, instead of `networksetup`.
- `vpn-conflict` fails when any tunnel device that is neither mihomo's TUN (its `tun.device`, or `Meta*` when unset) nor `tailscale*` holds the default route (`tun*`, `tap*`, `wg*`, `ppp*`, `ipsec*`, `vti*`, `xfrm*` and common vendor names), whether or not mihomo's TUN is on. A physical link holding it is a warning only when mihomo's TUN is on. On macOS only another `utun*` fails.

**Where checks run.** The helper runs them with `health.Scheduler`: every `--health-interval` (default 5 min), after an apply that wrote a file, after every service operation, and (macOS) on network changes from `darwin.NetWatcher`, debounced. `--no-health` turns it off. Only real status transitions publish `check.changed`; every run publishes `health.updated`. `twinnel helper health` reads `health.last`, `--run` calls `health.run`; `twinnel helper egress` calls `health.egress` (the egress test once; the helper logs every run's outcome, failures as warnings). While the helper is connected the app shows the helper's reports (`core/rpc/healthsrc`; a rerun calls `health.run`); without a helper it runs the checks in process. `twinnel doctor` and MCP's `doctor` ask the helper to run them (`health.run`; `--egress` adds `health.egress` when 定期检测 is off) and print that report, so they say what the app says: only the helper knows who holds the controller, the competitors' autostarts and the DNS it guards. They run the checks in process only with `--local`, with a flag that changes their inputs (`--controller`, `--mihomo-config`, `--sensitive-groups`, `--allow-exit-node`, `--egress-url`, `--egress=false`), or when the helper does not answer (a line on stderr); then they use the helper's settings (`health.Settings.ApplyUserChoices`; a flag given overrides), its controller credentials and, when it answers, its controller owner. `Report.Source` (`helper`, `local`, `local-no-helper`) says which, and a local report's text says so. `--only` filters the helper's report (`Report.Only`); `--only`/`--fix` reject unknown check ids with the list of valid ones. `twinnel status` rates its snapshot with the helper's `status.get` and `health.last` (`core/status.Evaluate`, like the app): another program's mihomo on the controller is an error and what it answers is not shown as Twinnel's. `controllerFor` carries that owner; a command that would change mihomo calls `writable()` first.

## Takeover

`twinnel takeover plan` turns a hand-made setup into Twinnel's model without changing anything. It finds the mihomo and Tailscale services (launchd plists on macOS, systemd units on Linux), reads the live config, scores candidate subscription files next to it, and derives layers: a `base` layer with the settings that differ from the subscription, `user` layers for hand-added rules, and the coexist layer in place of the hand-written Tailscale parts. It then composes them, runs `mihomo -t` with the service's own binary, and compares the result with the live config using a semantic diff (settings as sets where order does not matter, proxies by content hash, rules under first-match semantics). The output directory holds the layers, the composed config, the diff and a `plan.json` without credentials.

**Adopt** (`twinnel takeover plan --via-helper`, then `twinnel helper adopt PLAN_ID`; `core/engine/takeover.go`). The helper makes the plan itself and keeps it under `<state>/takeover/<id>/` (0700). Adopt re-checks that the live config is byte-identical to the plan, copies it to `live-backup.yaml`, imports the subscription (URL into the secret store), the layers and the settings (live binary, config, controller, service labels marked adopted: their plists are never replaced), runs five health checks, applies through the normal pipeline, and runs the checks again. A check that is still worse after one re-check restores the backup byte for byte, reloads and undoes the import. The step-by-step procedure is `docs/runbook-adopt-macos.md`.

**Harden** (`twinnel helper harden plan|apply`, `core/engine/harden.go`, macOS). After an adopt, mihomo still runs a binary and a config dir the user owns, as root (security review finding 5). Harden moves it, in one restart of mihomo, to the managed kernel, a root-owned config dir `<state>/mihomo` (the composed config, geo databases, `cache.db`, the dashboard) and the LaunchDaemon `com.l3ony2k.twinnel.mihomo`: bootout old, bootstrap new, wait for `/version`, compare health; on failure the old label is bootstrapped again. On success the old label is only disabled. Without root the helper prepares and validates, then reports the launchctl steps as a dry run.

## Services

`core/service.Manager` renders a definition and turns install / uninstall / start / stop / restart into a `Plan` of explicit steps (mkdir, write, copy, remove, run), which can be printed (`twinnel helper install --dry-run`) or applied with an executor; tests apply plans with fakes.

| | macOS (`platform/darwin/launchd`) | Linux (`platform/linux/systemd`) |
|---|---|---|
| definition | `/Library/LaunchDaemons/<label>.plist` | `/etc/systemd/system/<label>.service` |
| install | `bootout` (ignored), write, `enable`, `bootstrap system` | write, `daemon-reload`, `enable`, `restart` |
| stop | `bootout` (KeepAlive would restart a killed process) | `stop` |
| status | `launchctl print system/<label>` | `systemctl show <unit> --property=...` |
| keep alive | `KeepAlive`, `ThrottleInterval` | `Restart=always`, `RestartSec` |

The helper uses launchd on macOS and systemd on Linux, for its service RPCs and for `twinnel helper install`. Real `launchctl`/`systemctl` calls and file writes happen only when the helper runs as root.

**`twinnel helper install` / `uninstall`.** The plan is printed first (with the full plist or unit file), then confirmed (`--yes` without a terminal); `--dry-run` only prints.
- macOS: copy the binary to `/Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper`, create the state, socket and log dirs, `bootout` (ignored), write `/Library/LaunchDaemons/com.l3ony2k.twinnel.helper.plist`, `enable` (a label disabled earlier cannot be bootstrapped), `bootstrap`. As a normal user Twinnel re-runs itself as root through `osascript … with administrator privileges` (one password dialog; arguments quoted for the shell and for AppleScript, control characters refused).
- Linux: see [Linux](#linux). Run it with `sudo`; the non-root path tries macOS's `osascript` and fails.
- Uninstall removes the service definition, the binary and the socket; the state dir is kept.

## Linux

The helper and CLI pick these on Linux (`platform/linux/defaults.go`):

| What | Value |
|---|---|
| State dir | `/var/lib/twinnel` (the CLI maps its macOS flag default to it; an explicit `--state-dir` is kept) |
| Helper binary | `/usr/local/libexec/twinnel-helper` |
| Units | `twinnel-helper`, `twinnel-mihomo`, and the distro's `tailscaled`. On start the helper rewrites service labels still at their macOS defaults in `settings.json` to these names; labels you set are kept. |
| Socket group | `sudo` if it exists (Debian, Ubuntu), else `wheel` (Fedora, Arch, openSUSE), else `twinnel`, which the install plan creates with `groupadd --system`. The unit passes it as `--group`. A helper started by hand with the default `--group admin` falls back the same way when `admin` does not exist. |
| Tailscale interface | `ip route get 100.100.100.100` (normally `tailscale0`), for the helper's coexist resolver and `twinnel compose --tailnet-iface auto` |
| Secrets | `<state>/secrets.json`, 0600 |

`twinnel helper install` on Linux: create `/usr/local/libexec`, copy the binary, create the state and socket dirs, create the group if needed, write `/etc/systemd/system/twinnel-helper.service` (`Type=simple`, `Restart=always`, `RestartSec=5`, `WantedBy=multi-user.target`, no log files: stdout/stderr go to the journal), `systemctl daemon-reload`, `enable`, `restart`. Uninstall: `disable --now`, remove the unit, `daemon-reload`, remove the binary and the socket; the state dir and the group stay.

Not on Linux yet: a network-change trigger for health and tailnet refresh (the helper relies on its interval and the 60 s tailnet poller), the egress test, a Secret Service store, a non-root elevation path for `twinnel helper install`, and the app. Nothing of this has run on a real Linux host so far; it is covered by fixture tests that run on every OS.

## Windows

The Windows port (helper as the `TwinnelHelper` service, named pipe `\\.\pipe\twinnel-helper`, SCM plans, mihomo under `twinnel-svchost.exe`, DPAPI secrets, `platform/windows` health facts, UAC elevation) is described in [windows-design.md](windows-design.md). It is cross-compiled and tested with fakes; it has not run on a real Windows machine yet.

## Data locations

| What | Where |
|---|---|
| Helper state (settings, subscriptions, layers, history, kernels, tailnet cache) | macOS `/Library/Application Support/twinnel`, Linux `/var/lib/twinnel` (`--state-dir`). Files 0600, directories 0700. |
| Secrets (controller secret, subscription URLs) | macOS, helper as root: System keychain, generic passwords with account `twinnel` and service `twinnel.<key>` (e.g. `twinnel.controller-secret`). Otherwise (Linux, development helpers, `--secrets file`): `<state>/secrets.json`, 0600. Switching to the keychain moves the file's entries in and deletes the file. |
| Helper socket | `/var/run/twinnel/helper.sock` |
| Helper binary and service | macOS `/Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper`, LaunchDaemon `com.l3ony2k.twinnel.helper`; Linux `/usr/local/libexec/twinnel-helper`, unit `twinnel-helper.service` |
| Helper log | `<state>/logs/helper.log` (on Linux the helper's stdout/stderr also go to the journal). Every RPC is one line: method, caller (`peer.uid`/`peer.user` on macOS and Linux, `peer.sid`/`peer.user` on Windows, `peer.pid`), duration, error. The level comes from the call's class in `core/rpc/methods.go`, the allowlist itself: reads (what clients poll) at DEBUG, changes, secrets handed out and requested probes at INFO, `events.wait` never. A method with a read mode names in the same table which params make a call a read (`readWhen`): `tailscale.login` without `start`, `dry_run` of `config.import`/`takeover.undo`/`controller.move`, `preview` of `autostart.change`. `--log-level debug` shows the reads. |
| mihomo and tailscaled services | labels from settings: macOS `com.l3ony2k.twinnel.mihomo`, `com.tailscale.tailscaled`; Linux `twinnel-mihomo`, `tailscaled` |
| Managed mihomo kernels | `<state>/kernels/`, with `current` and `previous` symlinks; an optional seed kernel for the temporary download proxy at `<state>/kernels/seed/mihomo` |
| Tailscale installed by a setup | `<state>/tailscale/<version>/{tailscale,tailscaled}`, `current` symlink; LaunchDaemon `com.tailscale.tailscaled` |
| System DNS backup | `<state>/dns-backup.json` (what `dns.point` replaced; uninstall writes it back first) |
| Managed mihomo config dir (after harden) | `<state>/mihomo`, root-owned 0700; harden plans and the old plist copy in `<state>/harden/<id>/` |
| App preferences | `<user config dir>/twinnel/app.json` |
| Takeover plans | `twinnel takeover plan`: a new temp dir, or `--out`; `--via-helper`: `<state>/takeover/<id>/` (with `live-backup.yaml` after an adopt) |

## Security model

- **One privileged process.** Only the helper runs as root. The app and CLI read mihomo and Tailscale directly, and everything that writes goes through the helper.
- **Narrow RPC.** Fixed method allowlist, strict param decoding, peer uid check, no paths or commands from clients. Kernel installs take only a channel; the helper picks the asset and refuses one without a published SHA-256 digest.
- **Validate before write, back up before write, roll back on failure.** The running config is never replaced by one `mihomo -t` rejects.
- **Secrets.** The controller secret is kept out of history and backups and injected only into the live file. Subscription URLs are redacted in errors, logs, takeover plans and diffs. The config parser used by health checks never reads proxies or providers. On macOS the root helper keeps secrets in the System keychain; values are written by piping the command to `security -i` on stdin, so they never appear in argv, the environment or a file. On Linux they are a root-owned 0600 file until a Secret Service store exists. The app cannot read the System keychain (root-only): while the helper is connected it gets the controller address and secret through `controller.credentials`; without a helper it uses the secret saved in its `app.json`. The CLI follows the same rule (`status.Config.WithHelperCredentials`, `cmd/cli/helperconn.go controllerFor`): the helper's credentials, else app.json and the environment; `TWINNEL_CONTROLLER` pins the controller in both.
- **Never root for what a user controls.** A binary the helper runs (tailscale CLI, adopted mihomo, `mihomo -t`) runs as root only when root alone controls it (file and every parent dir); otherwise as its owner or nobody (`core/kernel/runas.go`). Writes into the mihomo config dir go through an `os.Root` and require the dir to belong to root or to the uid recorded at adopt (`mihomo.config_dir_uid`). Apply refuses a controller exposed beyond loopback without a secret. See `docs/security-review.md`.
- **Private files.** Every file that can hold credentials (the helper's store, the export zip, `app.json`, composed configs, takeover plans, `mihomo -t` scratch dirs) is written through `core/privfile`: owner-only from creation, 0600 / 0700 on Unix, an owner-only protected DACL on Windows (where a mode does nothing). A guard test refuses new direct 0600 writes.
- **Read-only diagnostics.** Health checks and takeover only read files, run read-only commands and send GET requests to the controller.
- **No personal data in the repo.** Test data is synthetic; `scripts/check-no-personal-data.sh` greps tracked files for patterns kept outside the tree.
