# Release builds

`scripts/build-release.sh` builds everything into `dist/` (see its header). This page covers what a
release build needs beyond a plain local build.

## The macOS 26 layered icon

Twinnel.app carries two icons:

- `AppIcon.icns` (`CFBundleIconFile`), from `assets/appicon/macos/twinnel.icns`. Every build has it;
  macOS 15 and earlier show it.
- `Assets.car` (`CFBundleIconName` = `twinnel`), compiled from the Icon Composer document
  `assets/appicon/twinnel.icon`. macOS 26 shows it in every appearance (Default, Dark, Clear, Tinted).

Compiling the `.icon` needs `actool` from Xcode 26 (`xcrun --find actool`). A Mac with only the
Command Line Tools has no `actool`: the build then prints a warning and the app has the `.icns`
only, which macOS 26 shows on a plain plate in Clear and Tinted mode. Release builds set
`TWINNEL_REQUIRE_LAYERED_ICON=1`, which turns that warning into an error, so a release never ships
without the layered icon by accident.

```sh
TWINNEL_REQUIRE_LAYERED_ICON=1 scripts/build-release.sh -v 1.2.3
```

Check a built app:

```sh
plutil -p dist/app/darwin-arm64/Twinnel.app/Contents/Info.plist | grep CFBundleIcon
xcrun assetutil --info dist/app/darwin-arm64/Twinnel.app/Contents/Resources/Assets.car | head
```

## Release workflow (GitHub Actions)

`.github/workflows/release.yml` runs on a pushed tag `v*` and by hand (Actions → release → Run
workflow, `dry_run` on by default). Three jobs:

| Job | Runner | What it does |
|---|---|---|
| `cli` | ubuntu-latest | `go test ./...`, then `scripts/build-release.sh --no-app` for darwin/arm64, darwin/amd64, linux/amd64, linux/arm64 (pure Go cross-builds); checks that `twinnel version --short` and `twinnel-helper -version` print the tag's version and that `install.sh --dry-run` plans the systemd unit |
| `app` | macos-26 (Xcode 26) | `build-release.sh --targets "" --app-platform darwin/universal` with `TWINNEL_REQUIRE_LAYERED_ICON=1`: one universal Twinnel.app (app, CLI and helper each arm64 + x86_64), ad-hoc signed, as `.dmg` and `.zip`; checks Assets.car, the architectures, the signature and the version |
| `release` | ubuntu-latest | merges the assets, writes `SHA256SUMS`, writes release notes from the commits since the previous `v*` tag, and creates a **draft** release (`--prerelease` for tags with a `-`, like `v0.2.0-rc1`). A rerun on the same tag replaces the draft's assets |

Version: the tag without the `v` (`v0.1.0` → `0.1.0`), stamped with `-ldflags -X` into
`core/version.Version` (CLI, app) and `core/engine.HelperVersion` (helper); `CFBundleShortVersionString`
is its numeric part. A dry run from a branch uses `git describe` (`0.0.0-<n>-g<hash>` before the
first tag).

Assets of `v0.1.0`:

```
Twinnel_0.1.0_macos_universal.dmg
Twinnel_0.1.0_macos_universal.zip
twinnel_0.1.0_darwin_arm64.tar.gz   twinnel, twinnel-helper, README.md, LICENSE, INSTALL.md
twinnel_0.1.0_darwin_amd64.tar.gz
twinnel_0.1.0_linux_amd64.tar.gz    the same, plus install.sh and twinnel-helper.service
twinnel_0.1.0_linux_arm64.tar.gz
SHA256SUMS
```

Signing stays ad hoc (no Developer ID, no notarization); `docs/install.md` tells users how to
open the app. The macOS job is the only one billed at 10x, so it builds just the app; Go
modules and the build cache are cached by `actions/setup-go`.

`packaging/linux/twinnel-helper.service` is a reference copy of the unit `twinnel helper install`
writes; `platform/linux/packaging_test.go` fails when they differ
(`TWINNEL_UPDATE_GOLDEN=1 go test ./platform/linux` rewrites it).

To cut a release:

```sh
git tag -a v0.1.0 -m "Twinnel 0.1.0"
git push origin v0.1.0
# wait for the run, then review and publish the draft on the Releases page
```
