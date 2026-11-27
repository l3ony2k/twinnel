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

## Design rules (from the owner)

- **Menu bar icon is monochrome only** (template image; macOS tints it). Never use color to carry state there. States are told apart by shape: a badge, a missing/broken part of the mark, a different glyph. Every state must read at 16 pt in both light and dark menu bars. (Other platforms: follow the native convention, e.g. monochrome tray icons on Windows 11 and GNOME.)
- **Sidebar selection follows the platform** (`plat.navSel`, cmd/app/nav.go). On macOS 26 and later it is Finder's: a neutral translucent fill, the label in the normal text color, icons in the accent on every row, no hover fill. Never fill the selected row with the accent there; that look is for macOS 15 and earlier only.
- **The cursor follows the platform.** The arrow on buttons, rows, disclosures, tiles, toggles, menus and tabs; the pointing hand only on real links (ui.Link); the I-beam on editable or selectable text. `cmd/app/cursor_test.go` fails on any other `ui.CursorPointer`.
- **Never steal the owner's focus.** The owner works on this Mac while agents run. Screenshot/dev runs of the app must not activate the app or show key windows on screen: no activation, windows shown inactive and off-screen (or rendered headless), no notifications. Check the frontmost app before/after a run.

## Copy rule (user, 2026-11-27)
Show users only what they need to know to act. No explanatory subtitles about internals (throttling windows, implementation details, "takes effect immediately"). If information is necessary, express it through UI state or layout (a disabled control, a badge, a status row), not a sentence. Multi-line secondary text must use the type token line height, never default spacing.
- **Dev stacks never point at the live mihomo.** A scratch helper's settings default the controller to 127.0.0.1:9090 (live). Always run shot/dev stacks with `DUET_CONTROLLER=<fake>` (it now wins over the helper's credentials) and seed the scratch helper's `mihomo.controller` to the fake one too. With `DUET_CONTROLLER` set, the app's in-process health checks read `DUET_MIHOMO_CONFIG` (else `$HOME/.config/mihomo/config.yaml`) instead of discovering the live LaunchDaemon config; keep `HOME` on a scratch dir. Shots must contain synthetic data only.
- **The owner's helper is installed and live** (`/var/run/duet/helper.sock`, LaunchDaemon `com.l3ony2k.duet.helper`, state `/Library/Application Support/duet`). Dev and shot stacks must use a scratch helper on a temp socket (`DUET_SOCKET`), never the live one. A shot run (`DUET_SHOTS`) refuses to start without a scratch `DUET_SOCKET`, a non-9090 `DUET_CONTROLLER`, `DUET_TAILSCALE_STATUS_FILE` and a scratch `HOME` (`cmd/app/shotguard.go`); everything else it reads comes from fixtures (`cmd/app/probes.go`). New code that reads the machine goes through `machineProbes`, never directly; recipe in CONTRIBUTING.md.
- **ⓘ for feature explanations** (owner, 2026-11-27): when a control or row needs explaining (what it is, what it changes, when to use it), put an `infoTip(c, title, body, more)` (cmd/app/infotip.go) after its label instead of a subtitle or paragraph. Body: one to three short sentences, zh + en. `more` can jump to the 关于 page for the long version. Use it where users would otherwise wonder "what does this do?" (managed layers, TUN, leak guard, coexist, mode, accepted exit node, takeover, harden, drift…), not on self-explanatory controls.
- **Tests never read the owner's real home/state.** Set `HOME`/state dirs to `t.TempDir()` in tests that touch `~/Library/...` paths.
