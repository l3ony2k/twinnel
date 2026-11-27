# duet

[简体中文](README.zh-CN.md)

duet runs the **official** [mihomo](https://github.com/MetaCubeX/mihomo) core and [Tailscale](https://tailscale.com) side by side on a desktop, and keeps checking that they still coexist: tailnet traffic goes to Tailscale, everything else goes through mihomo's rules, and DNS answers each name from the right place.

It is a manager for the cores you already trust. It is **not** another Clash client:

- It does not bundle or patch mihomo or Tailscale. It uses the official binaries: ones already installed, or mihomo releases it downloads from GitHub and verifies.
- Both run as system services (LaunchDaemons on macOS, systemd units on Linux). The app is only a control panel: if it quits or crashes, your network keeps working.
- **Neither core takes the system VPN slot.** mihomo uses its own TUN, the open-source `tailscaled` uses its own tun device, and duet generates the settings that make them coexist (route exclusions, DIRECT rules for Tailscale, MagicDNS routing) and verifies them with health checks.
- Your subscription stays read-only. Local changes (ports, DNS, your own rules, the Tailscale coexistence settings) are override layers composed on top of it, so a subscription update never loses them.

> **Status: early development.** macOS (arm64) is the first platform. On Linux the CLI, the health checks, the takeover planner and the helper (with systemd units) are wired up but have only been tested with fixtures, not on a real Linux host yet. Windows builds but has no platform layer. Interfaces and file formats may change.

## What works today

| Area | State |
|---|---|
| `duet doctor`: 13 coexistence health checks, text / Markdown / JSON | macOS, Linux |
| `duet takeover plan`: read a hand-made mihomo + Tailscale setup and derive override layers that reproduce it, proven with `mihomo -t` and a semantic diff | macOS, Linux (read-only) |
| Adopt through the helper (`duet takeover plan --via-helper`, `duet helper adopt`): exact backup, import, apply, health before/after, automatic restore; `duet helper secure-controller`; `duet helper harden`: move the adopted mihomo to the managed kernel and a root-owned config dir in one restart. Procedure: [docs/runbook-adopt-macos.md](docs/runbook-adopt-macos.md) | macOS. Rehearsed on a copy of a real setup with a non-root helper (dry run for launchctl); not yet run as root on the real one |
| `duet compose`: subscription + override layers → config, validated with `mihomo -t` | all |
| Root helper (`duet-helper`): compose → validate → back up → write → hot reload → verify, with automatic rollback; subscriptions, layers, history, mihomo kernel download / switch / rollback; geo databases and the external dashboard, verified against published digests | macOS, Linux |
| Health checks inside the helper: on a timer, after applies and service operations, and on network changes (macOS); `duet helper health` | macOS, Linux (no network-change trigger on Linux yet) |
| Service control over RPC: start / stop / restart mihomo and tailscaled, install / uninstall the mihomo service | macOS (launchd), Linux (systemd); without root the helper only reports the plan |
| `duet helper install` / `uninstall` / `upgrade` / `restart`: prints the plan (uninstall: what goes and what stays; `--purge` also deletes duet's data), then runs it as root. Uninstall never leaves mihomo without its service: after `harden` it moves mihomo back first (`--restore-mihomo`) | macOS: one administrator password dialog; Linux: run it with `sudo` (`--dry-run` needs no root). Not yet run on a real machine. |
| Menu bar app: monochrome status icon whose states differ by shape only (badge, broken ring, glyph), and a panel; main window with overview, proxy groups, Tailscale devices, health, logs, settings, and, through the helper, subscriptions and overrides, history, kernels; a first-run view while the helper is not installed | macOS; controls that change anything are disabled unless `DUET_ALLOW_WRITES=1` |
| Secrets (controller secret, subscription URLs) | macOS: System keychain when the helper runs as root; Linux and development helpers: a 0600 `secrets.json` in the state dir (Secret Service later) |

## Install from source

You need Go (the version in `go.mod`). No cgo, no other toolchain.

```sh
git clone https://github.com/l3ony2k/duet && cd duet
go build -o bin/duet ./cmd/cli          # CLI
go build -o bin/duet-helper ./cmd/helper  # root helper daemon
go run ./cmd/app                         # menu bar app (macOS), development run
```

Release archives (CLI + helper for macOS and Linux, plus `duet.app` and a disk image when built on a Mac) come from:

```sh
scripts/build-release.sh            # into dist/, with SHA256SUMS
scripts/build-release.sh --no-app   # CLI and helper only (any host)
```

The `.app` and the CLI and helper inside it are signed ad hoc (`SIGN_IDENTITY` and `NOTARY_PROFILE` switch to a Developer ID signature and notarization), not notarized. A copy you build yourself is not quarantined, so it opens directly; a downloaded one needs right-click → Open the first time.

### Daily use and development side by side

- **Daily:** `scripts/install-local.sh` builds the release, quits the running `/Applications/duet.app`, keeps a backup of it (`~/Library/Application Support/duet/backups`, newest 3), installs the new one, and runs `duet helper upgrade` only when the bundled helper differs from the installed one (one password prompt). This copy and the installed helper are the only ones with write access to the live system. 设置 → 后台助手 in the app shows the helper's version, updates, restarts or uninstalls it; 设置 → 通用 → 登录时打开 starts the app at login (packaged app only).
- **Development:** `go run ./cmd/app` against a development helper on a temp socket and a fake controller (`DUET_SOCKET`, `DUET_CONTROLLER`, see CONTRIBUTING), or read-only against the live setup (the default: writes stay off unless `DUET_ALLOW_WRITES=1`). A development build never asks for a helper update.

## CLI tour

Everything below except the helper commands is read-only.

```sh
duet doctor                      # run the health checks against the live setup
duet doctor --markdown           # a report to paste into an issue (it shows tailnet names: review it first)
duet doctor --only magicdns,system-dns --json
duet doctor --egress             # opt-in: one HTTPS request direct and one through mihomo, compared

duet takeover plan --out ./plan  # find the running services, pick the subscription the live config
                                 # came from, write layers + composed config + semantic diff to ./plan

duet compose --sub sub.yaml --base base.yaml --layer my-rules.yaml \
  --tailnet-from-status --leak-guard Google,Meta \
  --validate /path/to/mihomo -o config.yaml

duet status                      # mihomo + Tailscale snapshot (what the menu bar shows)
duet version                     # duet, helper, mihomo and Tailscale versions (read-only)
```

The helper commands talk to `duet-helper` over its Unix socket (`$DUET_SOCKET`, default `/var/run/duet/helper.sock`):

```sh
duet helper status
duet helper install --dry-run    # print the service definition and every step; changes nothing
duet helper install              # macOS: asks for an administrator password; Linux: sudo duet helper install
duet helper upgrade [--check]    # replace the installed helper with the one next to duet when it differs (no downgrade without --force)
duet helper restart              # restart only the helper
duet helper uninstall [--purge]  # lists what goes and what stays first; --purge also deletes settings, subscriptions, layers, history, keychain items
duet helper health [--run]       # the helper's last health report, or run the checks now
duet sub put main --url ...      # subscriptions: list | put | refresh | activate | rm
duet layer put rules rules.yaml  # override layers: list | get | put | rm | enable | disable | order
duet compose --preview           # compose and validate in the helper, no write
duet compose --apply             # validate, back up, write, reload, verify (rolls back on failure)
duet history                     # composed configs; duet history rollback ID
duet kernel check                # mihomo releases: list | check | install | rollback
duet kernel geo check            # geo databases: check | update; duet kernel ui check|update for the dashboard
duet service restart mihomo      # services: status | start | stop | restart (mihomo, tailscaled) | install | uninstall (mihomo)
duet settings                    # leak guard, coexist, tailnet resolver, history size, kernel channel, auto-apply
duet tailnet refresh             # re-read the peers now (the helper also polls every 60 s)
duet events --follow
```

Taking over a hand-made setup (step by step in [docs/runbook-adopt-macos.md](docs/runbook-adopt-macos.md)):

```sh
duet takeover plan --via-helper  # the helper makes the plan and keeps it; prints the plan id
duet helper plans [PLAN_ID]      # list or show kept plans
duet helper adopt PLAN_ID        # back up, import, apply, compare health; restores the backup if worse
duet helper secure-controller    # set a controller secret (active after mihomo's next start)
duet helper harden plan          # macOS: what moving to the managed kernel and a root-owned dir would do
duet helper harden apply         # one restart of mihomo, a few seconds offline
```

Commands that ask `Proceed? [y/N]` need `--yes` when stdin is not a terminal.

## Health checks

`tailnet-route`, `tun-route-exclude`, `tailscale-direct-rules`, `magicdns`, `peer-short-names`, `tailnet-resolver-binding`, `fake-ip-filter-mode`, `system-dns`, `vpn-conflict`, `peer-direct`, `leak-guard`, `leak-guard-egress` (opt-in), `controller-security`. Each reports a status, the evidence it read, and a fix hint. See [docs/architecture.md](docs/architecture.md#health-checks) for what each one checks.

## Safety model

- Only the helper runs as root, and every change to the system, the mihomo config or a binary goes through it. Its RPC methods are a fixed allowlist; none of them accepts a file path or a command from the client. Paths live in the helper's root-owned settings file, the only URLs a client can send are subscription URLs, and a kernel install takes only a release channel.
- The socket is mode 0660 for one group: `admin` on macOS; on Linux `sudo`, else `wheel`, else a `duet` group that the install creates. Each request's peer uid and groups come from the kernel's peer credentials. (On Linux these carry only the primary group for now, so a member of `sudo`/`wheel` whose primary group is different is refused; use root until that is fixed.)
- Before a config is written it must pass the official `mihomo -t`. The old file is backed up first, and if the reload or the post-reload check fails, the old config is restored.
- Mihomo kernels come only from the official GitHub releases, and their SHA-256 digests are checked before switching. The previous version is kept for rollback. Geo databases and the dashboard from GitHub releases are checked against the published digest too, and a geo update that `mihomo -t` rejects is rolled back.
- The controller secret and subscription URLs never go into history, logs or error messages. The egress test masks the public IPs it reports; other evidence in a report (tailnet names, peer endpoints) is shown as read, so look over a report before sharing it. The repository contains no personal data; `scripts/check-no-personal-data.sh` checks that against a pattern file kept outside the tree.

Details: [docs/architecture.md](docs/architecture.md). Reporting a vulnerability: [SECURITY.md](SECURITY.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). The product requirements and technical design are in `docs/requirements.md` and `docs/tech.md` (Chinese).

## License

MIT, see [LICENSE](LICENSE). Bundled icons: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
