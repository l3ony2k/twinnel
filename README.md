# duet

[简体中文](README.zh-CN.md)

duet runs the **official** [mihomo](https://github.com/MetaCubeX/mihomo) core and [Tailscale](https://tailscale.com) side by side on a desktop, and keeps checking that they still coexist: tailnet traffic goes to Tailscale, everything else goes through mihomo's rules, and DNS answers each name from the right place.

It is a manager for the cores you already trust. It is **not** another Clash client:

- It does not bundle or patch mihomo or Tailscale. It uses the official binaries: ones already installed, or mihomo releases it downloads from GitHub and verifies.
- Both run as system services (LaunchDaemons on macOS, systemd units on Linux). The app is only a control panel: if it quits or crashes, your network keeps working.
- **Neither core takes the system VPN slot.** mihomo uses its own TUN, the open-source `tailscaled` uses its own tun device, and duet generates the settings that make them coexist (route exclusions, DIRECT rules for Tailscale, MagicDNS routing) and verifies them with health checks.
- Your subscription stays read-only. Local changes (ports, DNS, your own rules, the Tailscale coexistence settings) are override layers composed on top of it, so a subscription update never loses them.

> **Status: early development.** macOS (arm64) is the first platform. Linux has the health checks and the takeover planner; service installation there is written but not wired into the helper yet. Windows builds but has no platform layer. Interfaces and file formats may change.

## What works today

| Area | State |
|---|---|
| `duet doctor`: 13 coexistence health checks, text / Markdown / JSON | macOS, Linux |
| `duet takeover plan`: read a hand-made mihomo + Tailscale setup and derive override layers that reproduce it, proven with `mihomo -t` and a semantic diff | macOS, Linux (read-only; there is no apply step yet) |
| `duet compose`: subscription + override layers → config, validated with `mihomo -t` | all |
| Root helper (`duet-helper`): compose → validate → back up → write → hot reload → verify, with automatic rollback; subscriptions, layers, history, mihomo kernel download / switch / rollback | runs on macOS; `duet helper install` only prints its plan (`--dry-run`) |
| Menu bar app: status icon and panel, main window (overview, proxy groups, Tailscale peers, health, logs, settings) | macOS; controls that change anything are disabled unless `DUET_ALLOW_WRITES=1` |
| Secrets in the system keychain | not yet: the helper keeps them in a 0600 file in its state dir |

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

The `.app` is signed ad hoc, not notarized. On first launch, right-click it and choose Open.

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
```

The helper commands talk to `duet-helper` over its Unix socket (`$DUET_SOCKET`, default `/var/run/duet/helper.sock`):

```sh
duet helper status
duet helper install --dry-run    # print the service definition and every step; changes nothing
duet sub put main --url ...      # subscriptions: list | put | refresh | activate | rm
duet layer put rules rules.yaml  # override layers: list | get | put | rm | enable | disable | order
duet compose --preview           # compose and validate in the helper, no write
duet compose --apply             # validate, back up, write, reload, verify (rolls back on failure)
duet history                     # composed configs; duet history rollback ID
duet kernel check                # mihomo releases: list | check | install | rollback
duet events --follow
```

## Health checks

`tailnet-route`, `tun-route-exclude`, `tailscale-direct-rules`, `magicdns`, `peer-short-names`, `tailnet-resolver-binding`, `fake-ip-filter-mode`, `system-dns`, `vpn-conflict`, `peer-direct`, `leak-guard`, `leak-guard-egress` (opt-in), `controller-security`. Each reports a status, the evidence it read, and a fix hint. See [docs/architecture.md](docs/architecture.md#health-checks) for what each one checks.

## Safety model

- Only the helper runs as root, and every change to the system, the mihomo config or a binary goes through it. Its RPC methods are a fixed allowlist; none of them accepts a file path or a command from the client. Paths live in the helper's root-owned settings file, the only URLs a client can send are subscription URLs, and a kernel install takes only a release channel.
- The socket is mode 0660 for the `admin` group, and each request's peer uid is checked by the kernel's peer credentials.
- Before a config is written it must pass the official `mihomo -t`. The old file is backed up first, and if the reload or the post-reload check fails, the old config is restored.
- Mihomo kernels come only from the official GitHub releases, and their SHA-256 digests are checked before switching. The previous version is kept for rollback.
- The controller secret and subscription URLs never go into history, logs or error messages. The egress test masks the public IPs it reports; other evidence in a report (tailnet names, peer endpoints) is shown as read, so look over a report before sharing it. The repository contains no personal data; `scripts/check-no-personal-data.sh` checks that against a pattern file kept outside the tree.

Details: [docs/architecture.md](docs/architecture.md). Reporting a vulnerability: [SECURITY.md](SECURITY.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). The product requirements and technical design are in `docs/requirements.md` and `docs/tech.md` (Chinese).

## License

MIT, see [LICENSE](LICENSE). Bundled icons: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
