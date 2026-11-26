# duet: rules for coding agents

Read `docs/requirements.md` and `docs/tech.md` first. They are written in Chinese; code, comments, commit messages and identifiers are in English. UI text is Chinese, kept in one place (`core/i18n` or the UI package's strings file) so English can be added later.

## Architecture (short)

- `cmd/app`: MyGo native-UI app (menu bar icon + flyout + main window). Thin: no business logic.
- `cmd/helper`: root daemon. All writes to the system, configs and binaries go through it.
- `cmd/cli`: command line client of the helper (and of read-only checks).
- `core/...`: platform-independent logic. Must not import `platform/...` directly; use interfaces.
- `platform/darwin/...`: macOS-only code (`//go:build darwin`). Linux/Windows come later.
- `overrides/`: built-in override templates.

Go 1.27, module `github.com/l3ony2k/duet`. Pure Go, no cgo. Run `gofmt`, `go vet ./...`, `go test ./...` before every commit.

## Hard safety rules (this runs on the owner's daily-driver Mac)

The machine runs a live mihomo (`~/mihomo-migration`, LaunchDaemon `com.mihomo.proxy`, controller `127.0.0.1:9090`) and tailscaled (brew, LaunchDaemon `sh.brew.tailscale`). Breaking them cuts the owner's network.

Allowed: reading anything; GET requests to the mihomo controller (including `/proxies/<name>/delay`); Tailscale LocalAPI / `tailscale status|ip|version|debug prefs` reads; `scutil`, `netstat`, `route get`, `networksetup -get*/-list*`, `dig @127.0.0.1`; running the official mihomo binary with `-t` on files **in a temp/scratch dir**.

Forbidden without the orchestrator's explicit approval: any write to `~/mihomo-migration`; mihomo controller PUT/PATCH/POST/DELETE (selecting nodes, changing mode, reloading); `tailscale up/down/set/login/logout`; `networksetup -set*`; `launchctl` load/unload/bootstrap/kickstart; anything with `sudo`; installing the helper. Test write paths against fakes (`httptest`, temp dirs) instead.

Secrets: the live config contains node credentials and the subscription URL. Never copy it, or any part of it, into the repo. Test data must be synthetic. For comparisons against the live config, work in `../../scratch/` (outside the repo) and commit only code.

## Working in a team

- Each agent works in its own git worktree and branch, and owns the paths named in its brief. Do not edit paths owned by another agent; if you need a change there, write it in your `HANDOFF.md`.
- Commit often on your branch with clear messages. Do not push, do not merge, do not rebase other branches.
- Do not add dependencies beyond what your brief allows without noting why in `HANDOFF.md`.
- When done (or blocked), write `HANDOFF.md` at the worktree root: what you built, how to run/test it, what is not done, open questions, and anything other agents must know. Then print a final line `AGENT-DONE` (or `AGENT-BLOCKED: <reason>`).
