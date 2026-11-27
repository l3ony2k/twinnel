# Release builds

`scripts/build-release.sh` builds everything into `dist/` (see its header). This page covers what a
release build needs beyond a plain local build.

## The macOS 26 layered icon

duet.app carries two icons:

- `AppIcon.icns` (`CFBundleIconFile`), from `assets/appicon/macos/duet.icns`. Every build has it;
  macOS 15 and earlier show it.
- `Assets.car` (`CFBundleIconName` = `duet`), compiled from the Icon Composer document
  `assets/appicon/duet.icon`. macOS 26 shows it in every appearance (Default, Dark, Clear, Tinted).

Compiling the `.icon` needs `actool` from Xcode 26 (`xcrun --find actool`). A Mac with only the
Command Line Tools has no `actool`: the build then prints a warning and the app has the `.icns`
only, which macOS 26 shows on a plain plate in Clear and Tinted mode. Release builds set
`DUET_REQUIRE_LAYERED_ICON=1`, which turns that warning into an error, so a release never ships
without the layered icon by accident.

```sh
DUET_REQUIRE_LAYERED_ICON=1 scripts/build-release.sh -v 1.2.3
```

Check a built app:

```sh
plutil -p dist/app/darwin-arm64/duet.app/Contents/Info.plist | grep CFBundleIcon
xcrun assetutil --info dist/app/darwin-arm64/duet.app/Contents/Resources/Assets.car | head
```

## Release job in GitHub Actions (draft, not enabled)

`.github/workflows/ci.yml` runs on pushes to `main` and on pull requests only, so a tag-only job
there would never run; adding a `tags:` trigger to it would also run every test job on tags.
When releases move to CI, add this as `.github/workflows/release.yml`. It only builds and
uploads the artifacts to the run; publishing a GitHub release and signing/notarizing with a
Developer ID (`SIGN_IDENTITY`, `NOTARY_PROFILE`, see the script header) are separate steps to add
with their secrets.

```yaml
name: release

on:
  push:
    tags: ['v*']

permissions:
  contents: read

jobs:
  build:
    runs-on: macos-26
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          fetch-depth: 0 # git describe --tags gives the version
      - uses: actions/setup-go@d8cbb8045d7f43706b9177cceb36c1f91a6644d5 # v7.1.0
        with:
          go-version-file: go.mod
      - name: select Xcode 26
        # the image's default Xcode may be older; pick the newest 26.x it has
        run: |
          xcode=$(ls -d /Applications/Xcode_26*.app | sort -V | tail -n 1)
          sudo xcode-select -s "$xcode"
          xcrun --find actool
          xcodebuild -version
      - name: build
        env:
          DUET_REQUIRE_LAYERED_ICON: "1"
        run: scripts/build-release.sh
      - name: check the layered icon
        run: |
          app=$(find dist/app -maxdepth 2 -name duet.app -type d | head -n 1)
          test -f "$app/Contents/Resources/Assets.car"
          plutil -extract CFBundleIconName raw "$app/Contents/Info.plist" | grep -qx duet
          plutil -extract CFBundleIconFile raw "$app/Contents/Info.plist"
      - uses: actions/upload-artifact@<pin a release SHA> # v5
        with:
          name: duet-${{ github.ref_name }}
          path: |
            dist/*.tar.gz
            dist/app/*/*.dmg
            dist/SHA256SUMS
```

Before enabling it: confirm the `macos-26` runner label and the Xcode 26 path on the current
runner image, and pin `actions/upload-artifact` to a SHA like the other actions.
