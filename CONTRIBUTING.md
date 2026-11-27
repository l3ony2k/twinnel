# Contributing to duet

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

### Screenshots (development)

`DUET_SHOTS=<dir> duet-app` renders every page and the flyout into PNGs, without showing a window or taking the focus, then quits (`cmd/app/shots*.go`; `DUET_SHOTS_FLOW`, `DUET_SHOTS_QUICK`, `DUET_SHOTS_WIDTH` pick other passes; `DUET_SHOTS_FLOW=toolbar` saves each page's toolbar idle, hovered and pressed). Run it against synthetic data only, so no real account, tailnet, device or node name ends up in a picture:

- mihomo: a fake controller (`DUET_CONTROLLER`, `DUET_MIHOMO_SECRET`) with invented groups and nodes;
- the helper: a development helper on a temp socket (`DUET_SOCKET`), started with `--tailnet-status-file cmd/app/testdata/shots-tailscale-status.json`;
- Tailscale: `DUET_TAILSCALE_STATUS_FILE=cmd/app/testdata/shots-tailscale-status.json`. The app (`core/tailscale.DefaultAPI`) and its in-process health checks then read that file instead of tailscaled's LocalAPI and `tailscale status --json`; Tailscale changes fail. Development only: the app logs it when the variable is set.

## Pull requests

- One topic per pull request, with a clear description of what changed and how you tested it.
- If you change what a health check reports, update its strings and `docs/architecture.md`.
- New dependencies need a reason in the pull request.
- Maintainers can run `scripts/check-no-personal-data.sh` with their own pattern file before merging.

By contributing you agree that your contribution is licensed under the MIT license.
