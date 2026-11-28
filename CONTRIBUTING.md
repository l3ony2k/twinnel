# Contributing to Twinnel

Thanks for helping. A few rules keep the project safe to run on a machine that depends on it.

## Before you start

- Read `docs/architecture.md`. The requirements and the technical plan are in `docs/requirements.md` and `docs/tech.md` (Chinese).
- Code, comments, identifiers and commit messages are in English. UI text is Chinese for now and lives in one place (`core/i18n`, or `core/health/strings.go` for the checks), so other languages can be added.

## Layout rules

- `core/...` is platform-independent and must not import `platform/...`. Put OS-specific code in `platform/<os>/` and reach it through an interface defined in core (`health.Env`, `service.Manager`, `takeover.Probe`).
- Keep `cmd/app`, `cmd/cli` and `cmd/helper` thin. Logic belongs in `core`.
- Pure Go, no cgo. Every target must cross-compile from any host.
- The menu bar icon is monochrome (a template image). States differ by shape, never by color.

## Testing

```sh
gofmt -l .        # must print nothing
go vet ./...
go test ./...
GOOS=linux go build ./... && GOOS=windows go build ./...
```

CI runs the same on macOS and Ubuntu, plus cross-builds.

- Tests must not change the machine they run on. Test write paths against fakes: `httptest` servers for the mihomo controller, temp dirs for files, a fake `service.Executor` for service plans. Platform parsers are tested on fixtures in `testdata/`.
- Tests against a real mihomo or tailscaled are behind the `live` build tag or an environment variable, and stay read-only.
- Test data must be synthetic: documentation IP ranges (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24), `example.com`, an invented tailnet name. Never paste a real config, subscription, node, tailnet name or peer list into the repo, an issue or a test.

### Writes and `TWINNEL_READ_ONLY`

A control is enabled when what it acts on can act: mihomo's controller answers (mode, node, TUN), Tailscale's LocalAPI answers (connect, exit node), the helper is connected (config, services, kernel). There is no switch to turn writes on. `TWINNEL_READ_ONLY=1` turns every write off: use it for every development or agent run that is near the live setup, and in any script that starts the app.

### Daily copy and development copy

Keep two copies apart. The **daily** copy is `/Applications/Twinnel.app` plus the installed helper (`/Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper`): the only ones with write access to the live system. Update both with `scripts/install-local.sh` (build, back up, replace, `twinnel helper upgrade` only when the helper changed). The **development** copy runs from source: `go run ./cmd/app` against a development helper (`go run ./cmd/helper --state-dir <tmp> --socket <tmp>/twinnel.sock --foreground`, then `TWINNEL_SOCKET=<tmp>/twinnel.sock`) and a fake controller, or read-only against the live setup with `TWINNEL_READ_ONLY=1`. A development build (`version.Version` contains "dev") never reports a helper version mismatch. Test `install-local.sh` with `--dest <tmp> --backups <tmp> --no-helper`, never against `/Applications` in an automated run.

`登录时打开` (MyGo `SetOpenAtLogin`, SMAppService) needs an app bundle: a `go run` build shows it disabled with 需打包版. An ad-hoc-signed copy in `/Applications` registers; macOS may ask to allow it in System Settings › General › Login Items, which the app says when the call reports it.

### What a launch opens (development, real-machine GUI QA)

One decision for every platform (`cmd/app/launch.go`). A normal launch shows only the menu bar or tray icon; the first launch of a machine that is not set up opens the wizard once the helper's state is known (`launchOpensWizard`). A run against a scratch helper (`TWINNEL_SOCKET` set) never opens a window by itself, so a first-run test with a scratch socket shows nothing until it asks:

| Ask | Opens |
|---|---|
| `TWINNEL_OPEN=1` | the main window as a real launch shows it: the wizard when the machine is not set up, else 总览 |
| `TWINNEL_OPEN=panel` | the main window and the flyout |
| `TWINNEL_OPEN=page:<id>` | the main window at that page and nothing over it; `page:wizard` is the wizard |
| `TWINNEL_WIZARD=1` / `0` | forces the first-run wizard on or off, whatever else is set |

Page ids: `overview proxies tailscale health logs config history kernel takeover settings about install leave wizard` (`allPages` in `cmd/app/window.go`; an unknown id is logged with the list and the main window opens at its default page). The same values work as an argument, `--open=page:kernel`; on Windows a second launch passes it to the running copy, which turns its window to that page, so a QA script can step through pages without restarting the app or clicking by coordinates: `Twinnel.exe --open=page:health`. These hooks show and focus a window: use them on a test machine, never on a machine someone is working at (use shots there).

### Screenshots (development)

`TWINNEL_SHOTS=<dir> twinnel-app` renders every page and the flyout into PNGs without taking the focus, then quits (`cmd/app/shots*.go`; `TWINNEL_SHOTS_FLOW`, `TWINNEL_SHOTS_QUICK`, `TWINNEL_SHOTS_WIDTH` pick other passes; `TWINNEL_SHOTS_FLOW=toolbar` saves each page's toolbar idle, hovered and pressed; `TWINNEL_SHOTS_FLOW=helpermgmt` saves 设置 → 后台助手 in each state and the 需要更新后台助手 row). Shots are read-only by default (as `TWINNEL_READ_ONLY=1`); a flow that writes against a fake helper or controller sets `TWINNEL_READ_ONLY=0` and `TWINNEL_SHOTS_WRITE=1`. Run it against synthetic data only, so no real account, tailnet, device or node name ends up in a picture.

On macOS and Linux the windows stay hidden and are captured from memory. On Windows a hidden window draws no frame after its first, so every capture would repeat it: there each shot window is moved far off screen (−32000) and shown without activation before its first capture (`cmd/app/shots_window.go`). It may show a taskbar button for the run; it never takes the focus. Every run hashes what it saves: two shot names with the same picture (a stale capture, or a step that changed nothing) are logged as `SAME PICTURE AS` and the run exits 1. `TestWindowsShots` checks its pages the same way.

`go test ./cmd/app -run TestWindowsShots` with `TWINNEL_WINSHOTS=<dir>` (and `TWINNEL_WINSHOTS_PAGES`, `TWINNEL_WINSHOTS_SCALE`, default 1.5, `TWINNEL_WINSHOTS_WIDTH` in DIPs) renders the Windows layout (Fluent `plat`, Windows fixtures) headless from any machine: a first look before a real-PC round.

A shot run is hermetic by construction: `shotGuard` (`cmd/app/shotguard.go`) stops it at startup unless it names a scratch helper socket (`TWINNEL_SOCKET`, never `/var/run/twinnel/helper.sock`), a fake controller (`TWINNEL_CONTROLLER`, never port 9090), a synthetic tailnet (`TWINNEL_TAILSCALE_STATUS_FILE`) and a scratch `HOME` (not your real home). Every other read of the machine goes through `machineProbes` (`cmd/app/probes.go`), which in shot mode answers from fixtures: the in-process health checks read a `core/health/fixture` machine (`TWINNEL_HEALTH_FIXTURE=<file.json>`, else `fixture.Synthetic()`, whose tailnet is the status file's), live-setup detection finds nothing (flows set it), the Tailscale install is synthetic, and the `twinnel` CLI is never run (version, install plan and upgrade check are canned; every other command fails). Health comes from the fixture even when the helper is connected. `TestShotFlowsAreHermetic` fails if any flow's setup reaches the host probes.

Recipe (from the repo root; `S` is a scratch dir outside the repo):

```sh
S=${TWINNEL_SCRATCH:-$HOME/.cache/twinnel-dev}/shots; TS=$PWD/cmd/app/testdata/shots-tailscale-status.json
mkdir -p $S/home $S/state $S/empty $S/out
go build -o $S/fakemihomo ./cmd/app/internal/fakemihomo && go build -o $S/devfake ./cmd/helper/internal/devfake
go build -o $S/twinnel-helper ./cmd/helper && go build -o $S/twinnel-app ./cmd/app
$S/fakemihomo --listen 127.0.0.1:19894 &                   # fake mihomo controller: invented groups, nodes, traffic, logs
$S/devfake --listen 127.0.0.1:19895 --write-mihomo $S/fakebin &  # writes the fake `mihomo -t` (mihomo, or mihomo.cmd on Windows)
$S/twinnel-helper --state-dir $S/state --socket $S/twinnel.sock --foreground --secrets file --group staff \
  --tailnet-status-file $TS --no-health --no-controller-owner --tailnet-iface none \
  --takeover-services-dir $S/empty --takeover-home $S/home &   # scratch helper: no host health, takeover probe or route lookups
# set "controller": "127.0.0.1:19894" and "binary": "$S/fakebin/mihomo" under "mihomo" in $S/state/settings.json, then restart the helper
TWINNEL_SHOTS=$S/out TWINNEL_SHOTS_FLOW=status TWINNEL_SOCKET=$S/twinnel.sock TWINNEL_CONTROLLER=127.0.0.1:19894 \
  TWINNEL_TAILSCALE_STATUS_FILE=$TS HOME=$S/home $S/twinnel-app
```

`devfake`'s own controller answers only `/version` and `PUT /configs` (what the helper's tests need); pages want `fakemihomo`. `scripts/readme-shots.sh` is the full version of this recipe. On Windows the scratch helper serves its pipe (`--socket \\.\pipe\twinnel-dev-<name>`) without elevation; keep it unelevated next to the live services.

The README's images (`docs/images/`) come from `scripts/readme-shots.sh <scratch dir>` (`TWINNEL_SHOTS_FLOW=readme`, fixtures in `cmd/app/testdata/readme`): it builds the same hermetic stack, seeds the scratch helper with a synthetic subscription and layers, shoots English and Chinese in light and dark, and frames and compresses the PNGs. Look at every image before committing it.

## Names from before the rename

The project was called duet until 2026-11-27. `core/legacy` holds the old names; the only code that uses them finds and migrates an old install (`cmd/cli/helper_migrate.go`, the app's `legacyprefs.go`, the keychain's legacy read, `rpc.SocketFromEnv`'s fallback, uninstall). `DUET_*` environment variables are copied to their `TWINNEL_*` names at start (with a deprecation notice) for one release. Two stored formats keep the old spelling on purpose: the history index field `before_duet` and the keychain value marker `duet-b64:`. Drop the rest in the release after the first public one.

## Pull requests

- One topic per pull request, with a clear description of what changed and how you tested it.
- If you change what a health check reports, update its strings and `docs/architecture.md`.
- New dependencies need a reason in the pull request.
- Maintainers can run `scripts/check-no-personal-data.sh` with their own pattern file before merging.

By contributing you agree that your contribution is licensed under the MIT license.
