# Publishing Twinnel: pre-publish checklist

Everything here is still to do: no remote exists, nothing has been pushed, and the history has
not been rewritten. The numbers come from read-only scans of a mirror clone made on 2026-11-27
(408 commits, 79 branches). Run them again right before publishing.

`P` below is the private pattern file kept outside the repo (`../../scratch/private-patterns.txt`
from a worktree). Never print it, commit it, or paste its lines into an issue.

## 1. Decisions for the owner

| Decision | Today | Where it shows up |
|---|---|---|
| Project name | **decided: Twinnel** (twin + tunnel; was "duet" until 2026-11-27) | everywhere; section 7 lists the identifiers |
| Owner / org on GitHub | **decided: personal account `l3ony2k`**, repo `github.com/l3ony2k/twinnel` (no remote added yet) | module path, bundle id, launchd labels |
| Visibility | private, local only | — |
| Bundle id | `io.github.l3ony2k.twinnel` (app `Twinnel.app`) | `cmd/app/mygo.json` |
| launchd labels | `com.l3ony2k.twinnel.helper`, `com.l3ony2k.twinnel.mihomo` | helper, CLI, uninstall, docs |
| Go module path | `github.com/l3ony2k/twinnel` | `go.mod`, every import |
| Branches to publish | 79 local branches (`feat/*`, `fix/*`, `docs/*`) | publish `main` only (and tags), not the agent branches |
| Author identity | one name and one email on every commit | decide whether to publish that email or rewrite it to a GitHub `noreply` address (`--mailmap`) |
| Release channel | none | signing and notarization, section 5 |

Changing the launchd labels after people have installed the helper needs a migration. The
rename from duet did exactly that (`cmd/cli/helper_migrate.go`, names in `core/legacy`): drop
that code, and `core/legacy`, in the release after the first public one.

## 2. Backup (before touching history)

```sh
B=~/twinnel-backup-$(date +%Y%m%d)          # outside the repo and the task folder
mkdir -p "$B"
git -C <repo> bundle create "$B/all.bundle" --all
git clone --mirror <repo> "$B/mirror.git"
tar -czf "$B/worktrees.tgz" -C <task folder> repo wt   # working trees, untracked files included
git bundle verify "$B/all.bundle"
```

Keep the backup until the published repository has been cloned back and built.

## 3. History cleanup (git filter-repo)

### What to remove

Build outputs that were committed early on. Current `.gitignore` already covers all of them;
`HEAD` contains none.

| Path | Versions in history | Size of those blobs |
|---|---|---|
| `cmd/app/app` | 13 | 337 MB |
| `app` | 12 | 295 MB |
| `app.exe` | 2 | 48 MB |
| `helper` | 2 | 27 MB |
| `cli` | 1 | 14 MB |
| `fakemihomo` | 1 | 9 MB |

No other blob over 1 MB exists in history. The pack is 283 MiB today; after the cleanup it is
2.1 MiB (dry run).

### Private strings

Scanned with `P` across every commit (file contents, file names, commit messages):

- `HEAD` and every current branch tip are clean (`scripts/check-no-personal-data.sh P`).
- Older history has two hits: one private address in an earlier version of
  `cmd/app/internal/fakemihomo/main.go` (13 commits) and one private device name in an earlier
  `core/config/semdiff_test.go` (5 commits).
- Commit messages and file names: no hits.
- Commit author and committer: one name and one email on all commits (decision in section 1).

The replacement file is generated from `P` and stays in scratch, never in the repo:

```sh
python3 - P replace-text.txt <<'EOF'
import re, sys
src, dst = sys.argv[1], sys.argv[2]
out = []
for l in open(src):
    l = l.strip()
    if not l or l.startswith("#"):
        continue
    b = r"\b" if len(l) < 5 else ""          # short patterns: whole words, as the check script
    out.append(f"regex:(?i){b}{re.escape(l)}{b}==>redacted")
open(dst, "w").write("\n".join(out) + "\n")
EOF
```

### Dry run on a mirror clone in scratch

```sh
S=<scratch>/publish
git clone --mirror <repo> $S/filtered.git
cd $S/filtered.git
git filter-repo --force --invert-paths \
  --path app --path app.exe --path cmd/app/app --path helper --path cli --path fakemihomo \
  --strip-blobs-bigger-than 5M \
  --replace-text $S/replace-text.txt
# optional: --mailmap $S/mailmap  (author email → GitHub noreply)
git gc --prune=now --aggressive
git count-objects -vH                                   # size-pack ≈ 2 MiB
```

Verify:

```sh
git rev-parse main^{tree}        # same as the original main's tree: HEAD's files are unchanged
git rev-list --all | xargs git grep -l -I -i -F -f <long patterns>  # nothing
gitleaks git --redact $S/filtered.git                    # only the known false positives (section 4)
git clone $S/filtered.git $S/check && cd $S/check && go vet ./... && go test ./...
```

On 2026-11-27 this ran in about 5 s, left 0 pattern hits and 0 blobs over 1 MB, and `main`'s tree
was byte-identical to the original.

### For real

Do it on a fresh mirror clone, never on the working repository. Push only `main` (and tags) to
the new remote. Every commit id changes: worktrees and branches made from the old history must be
recreated from the new one.

## 4. Secrets scan

Run on the full history of a mirror clone, with `--redact`:

- `gitleaks git --redact` (8.30.1): 30 findings, all `generic-api-key` on `nodekey:` values in two
  synthetic Tailscale status fixtures (`core/agent/testdata/tailscale-status.json`,
  `cmd/app/testdata/shots-tailscale-status.json`). Tailscale node keys are public keys, and these
  are invented. Add a `.gitleaks.toml` allowlist for those two paths (or for `nodekey:[0-9a-f]{64}`)
  so a CI scan stays quiet.
- `trufflehog git --no-verification`: 0 findings.
- No `.env`, key or certificate files anywhere in history.

## 5. Licence and third-party notices

- Twinnel: MIT (`LICENSE`, "Copyright (c) 2026 Leon and Claude").
- `THIRD_PARTY_NOTICES.md` covers the Kubai Icons (MIT). It does not yet cover the Go modules
  compiled into the binaries. All of them are permissive, but MIT, BSD and ISC require their
  notices in binary distributions:

| Licence | Modules |
|---|---|
| MIT | `github.com/egoist/mygo` (UI), `github.com/modelcontextprotocol/go-sdk` (MCP), `gopkg.in/yaml.v3` (MIT and Apache-2.0), `github.com/google/jsonschema-go`, `github.com/fxamacker/cbor/v2`, `github.com/mitchellh/go-ps`, `github.com/segmentio/asm`, `github.com/segmentio/encoding`, `github.com/x448/float16` |
| BSD-3-Clause | `tailscale.com`, `golang.org/x/{crypto,exp,image,net,oauth2,sync,sys,text,time}`, `filippo.io/edwards25519`, `github.com/go-json-experiment/json`, `github.com/hdevalence/ed25519consensus`, `github.com/yosida95/uritemplate/v3`, `go4.org/netipx` |
| Apache-2.0 | `github.com/ebitengine/purego`, `go4.org/mem` (no NOTICE files) |
| ISC | `github.com/coder/websocket` |
| Unlicense or BSD-3-Clause | `github.com/go-text/typesetting` |

  To do: generate the notices at release time (e.g. `go-licenses report` / `save` over
  `./cmd/app ./cmd/helper ./cmd/cli`), ship them in the `.app` (`Contents/Resources`) and in the
  CLI archives, and add a pointer from `THIRD_PARTY_NOTICES.md`.
- mihomo and Tailscale binaries are not bundled (Twinnel downloads official mihomo releases at the
  user's request), so their licences do not apply to Twinnel's archives.
- Kubai Icons: confirm the upstream licence file still says MIT at the commit the icons came from.

## 6. CI, signing and the icon

- CI (`.github/workflows/ci.yml`): gofmt, vet and tests on macOS and Ubuntu, plus cross-builds for
  Windows, linux/arm64 and darwin/amd64. It has never run on GitHub, since there is no remote.
  After the first push, check that `macos-latest` has the Go version in `go.mod` (1.27.4) and that
  the tests pass without a GUI session.
- Add a gitleaks job (with the allowlist from section 4) and, for maintainers, a step that runs
  `scripts/check-no-personal-data.sh` with a pattern file from a repository secret.
- Signing: builds are signed ad hoc. A public download needs a Developer ID Application
  certificate and notarization: `SIGN_IDENTITY` and `NOTARY_PROFILE` in `scripts/build-release.sh`
  (hardened runtime on `Twinnel.app`, the CLI and the helper; the helper's launchd plist points at a
  path under `/Library/PrivilegedHelperTools`, so the installed copy must carry the same
  signature). In CI that means the certificate and an App Store Connect API key as secrets and a
  temporary keychain. Until then, release notes must say "right-click → Open".
- Layered icon: `Assets.car` needs `actool` from Xcode 26. The draft release job in
  `docs/release.md` runs on `macos-26` with `TWINNEL_REQUIRE_LAYERED_ICON=1`; enable it as
  `.github/workflows/release.yml` once tags are pushed.

## 7. Rename checklist (if the name or owner changes again)

Counted on 2026-11-27 with `git grep -c`:

| Identifier | Files |
|---|---|
| `github.com/l3ony2k/twinnel` (module path, imports) | 266 files: `go.mod`, almost every `.go` file, the `-X` ldflags in `scripts/build-release.sh` (`go mod edit -module`, then rewrite the imports) |
| `io.github.l3ony2k.twinnel` (bundle id) | `cmd/app/mygo.json` |
| `com.l3ony2k.twinnel.helper` / `.mihomo` (launchd labels, plist names; 14 and 10 files) | `core/store/settings.go`, `cmd/helper`, `cmd/app/helper.go`, `cmd/app/leavepage.go`, `core/uninstall`, `core/engine`, tests, `cmd/app/testdata/shots-install-plan.txt`, `docs/architecture.md`, `docs/runbook-adopt-macos.md`, `docs/security-review.md`, `CONTRIBUTING.md`, `AGENTS.md` |
| `/var/run/twinnel/helper.sock`, `/Library/Application Support/twinnel`, `/Library/PrivilegedHelperTools/…`, `~/Library/Application Support/twinnel` | helper, CLI and app defaults, docs |
| `twinnel` as a command, app and archive name (`twinnel`, `twinnel-helper`, `Twinnel.app`) | `scripts/build-release.sh`, `scripts/install-local.sh`, README, docs |
| `TWINNEL_*` environment variables (`DUET_*` still read for one release, `core/legacy`) | code, CONTRIBUTING, AGENTS |
| `git clone https://github.com/l3ony2k/twinnel` | `README.md`, `README.zh-CN.md` |

Then: regenerate the README screenshots (`scripts/readme-shots.sh`), since the app shows its name.

## 8. Last look before the push

- [ ] Backup made and verified (section 2)
- [ ] Owner decisions made (section 1)
- [ ] Rewritten mirror verified (section 3), only `main` and tags pushed
- [ ] `scripts/check-no-personal-data.sh P` clean on the final tree
- [ ] gitleaks and trufflehog on the final history: only the allowlisted fixtures
- [ ] Every image under `docs/images/` looked at by a person (synthetic data only)
- [ ] Notices generated for the binaries (section 5)
- [ ] CI green on the new remote
- [ ] `HANDOFF.md`, `scratch/` and other local-only files are not in the tree (`HANDOFF.md` is ignored)
