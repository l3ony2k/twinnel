#!/usr/bin/env bash
# Build release artifacts into dist/ (gitignored):
#   dist/duet_<version>_<os>_<arch>.tar.gz   the duet CLI and duet-helper
#   dist/app/                                 duet.app + .dmg (macOS hosts only)
#   dist/SHA256SUMS
#
# Usage: scripts/build-release.sh [-v VERSION] [--no-app] [--targets "darwin/arm64 linux/amd64"]
#
# Pure Go, no cgo: every target cross-compiles from any host. The .app needs
# `mygo build` (run through `go run`, pinned to the version in go.mod) and,
# for signing and the disk image, a macOS host.
#
# Signing: the embedded duet and duet-helper, then the .app, are signed with
# $SIGN_IDENTITY: "-" (the default) is an ad-hoc signature, enough for a
# self-built copy that Gatekeeper does not quarantine (built locally, never
# downloaded). For distribution set SIGN_IDENTITY to a "Developer ID
# Application: …" identity (adds the hardened runtime and a timestamp) and
# NOTARY_PROFILE to a `xcrun notarytool store-credentials` profile: the
# disk image is then notarized and stapled.
#
# Version: -v, else `git describe --tags`; an untagged checkout gets
# 0.0.0-<commits>-g<hash>[-dirty], which orders like a describe of a tag.
#
# Icon: with Xcode 26's actool the app also gets the macOS 26 layered icon
# (assets/appicon/duet.icon → Assets.car); DUET_REQUIRE_LAYERED_ICON=1 fails
# the build without it (docs/release.md).
set -euo pipefail

cd "$(dirname "$0")/.."

version=""
build_app=1
targets="darwin/arm64 darwin/amd64 linux/amd64 linux/arm64"
while [ $# -gt 0 ]; do
	case "$1" in
	-v | --version) version="$2"; shift 2 ;;
	--no-app) build_app=0; shift ;;
	--targets) targets="$2"; shift 2 ;;
	-h | --help) sed -n '2,13p' "$0"; exit 0 ;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done
if [ -z "$version" ]; then
	if version="$(git describe --tags --dirty 2>/dev/null)"; then
		version="${version#v}"
	elif hash="$(git rev-parse --short HEAD 2>/dev/null)"; then
		version="0.0.0-$(git rev-list --count HEAD)-g${hash}"
		git diff --quiet HEAD 2>/dev/null || version="${version}-dirty"
	else
		version="dev"
	fi
fi
sign_identity="${SIGN_IDENTITY:--}"

# The macOS 26 layered icon (assets/appicon/duet.icon, Icon Composer) needs
# actool from Xcode 26. With it, the .icon is compiled to Assets.car in
# Contents/Resources and named by CFBundleIconName; AppIcon.icns stays as
# CFBundleIconFile for older macOS. Without it the app keeps the .icns only
# and the build warns; DUET_REQUIRE_LAYERED_ICON=1 (release CI) makes that
# an error.
layered_icon=assets/appicon/duet.icon
add_layered_icon() {
	local app="$1" out msg
	if ! xcrun --find actool >/dev/null 2>&1; then
		if [ "${DUET_REQUIRE_LAYERED_ICON:-0}" = 1 ]; then
			echo "error: DUET_REQUIRE_LAYERED_ICON=1 but actool (Xcode 26) is not available" >&2
			exit 1
		fi
		echo "    warning: no actool (Xcode 26): duet.app gets the .icns only, no layered macOS 26 icon" >&2
		return 0
	fi
	out="$dist/.actool"
	rm -rf "$out" && mkdir -p "$out"
	# --minimum-deployment-target 26.0: the catalog carries only the layered
	# icon; macOS 15 and earlier take CFBundleIconFile (our drawn .icns).
	if ! msg="$(xcrun actool "$layered_icon" --compile "$out" --output-format human-readable-text \
		--platform macosx --target-device mac --minimum-deployment-target 26.0 \
		--app-icon duet --output-partial-info-plist "$out/partial.plist" 2>&1)" || [ ! -f "$out/Assets.car" ]; then
		echo "$msg" >&2
		if [ "${DUET_REQUIRE_LAYERED_ICON:-0}" = 1 ]; then
			echo "error: actool could not compile $layered_icon" >&2
			exit 1
		fi
		echo "    warning: actool could not compile $layered_icon: duet.app gets the .icns only" >&2
		rm -rf "$out"
		return 0
	fi
	cp "$out/Assets.car" "$app/Contents/Resources/Assets.car"
	plutil -replace CFBundleIconName -string duet "$app/Contents/Info.plist"
	rm -rf "$out"
	echo "    layered icon: Assets.car (CFBundleIconName duet), .icns kept for macOS 15 and earlier"
}

dist=dist
rm -rf "$dist"
mkdir -p "$dist"

# core/version.Version is duet's version (CLI, app); the helper reports
# engine.HelperVersion.
vflags="-X github.com/l3ony2k/duet/core/version.Version=${version} -X github.com/l3ony2k/duet/core/engine.HelperVersion=${version}"
ldflags="-s -w ${vflags}"

for t in $targets; do
	os="${t%/*}"
	arch="${t#*/}"
	name="duet_${version}_${os}_${arch}"
	out="$dist/$name"
	mkdir -p "$out"
	echo "==> $os/$arch"
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$out/duet" ./cmd/cli
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$out/duet-helper" ./cmd/helper
	cp LICENSE README.md "$out/" 2>/dev/null || true
	tar -C "$dist" -czf "$dist/$name.tar.gz" "$name"
	rm -rf "$out"
done

if [ "$build_app" = 1 ]; then
	if [ "$(uname -s)" = Darwin ]; then
		# duet.app ships the CLI and the helper in Contents/Resources/bin, so
		# the first-run 安装后台助手 button works from a packaged app (the app
		# runs bin/duet helper install --helper-bin bin/duet-helper).
		# Contents/MacOS/duet is the app itself. mygo copies the project's
		# resources/<goos>-<goarch>/ into the bundle and signs the binaries.
		app_platform="darwin/arm64"
		app_arch="${app_platform#*/}"
		res="cmd/app/resources"
		app_json="cmd/app/mygo.json"
		app_json_orig="$dist/.mygo.json.orig"
		cp "$app_json" "$app_json_orig"
		cleanup_app() {
			cp "$app_json_orig" "$app_json" && rm -f "$app_json_orig"
			rm -rf "$res"
		}
		trap cleanup_app EXIT
		rm -rf "$res"
		mkdir -p "$res/darwin-$app_arch/bin"
		for cmd in cli:duet helper:duet-helper; do
			CGO_ENABLED=0 GOOS=darwin GOARCH="$app_arch" go build -trimpath -ldflags "$ldflags" \
				-o "$res/darwin-$app_arch/bin/${cmd#*:}" "./cmd/${cmd%:*}"
		done
		# CFBundleShortVersionString wants numbers: 1.2.3 from v1.2.3-4-gabc,
		# 0.0.0 for an untagged build.
		bundle_version="$(printf '%s' "$version" | sed -nE 's/^([0-9]+(\.[0-9]+){0,2}).*/\1/p')"
		bundle_version="${bundle_version:-0.0.0}"
		awk -v v="$bundle_version" '
			/^[[:space:]]*"version":/ { next }
			{ print }
			/^[[:space:]]*"name":/ && !done { match($0, /^[[:space:]]*/); printf "%s\"version\": \"%s\",\n", substr($0, 1, RLENGTH), v; done = 1 }
		' "$app_json_orig" >"$app_json"
		grep -q "\"version\": \"${bundle_version}\"" "$app_json" || { echo "could not set the app version in $app_json" >&2; exit 1; }
		mygo_version="$(go list -m -f '{{.Version}}' github.com/egoist/mygo)"
		echo "==> duet.app ${bundle_version} (mygo $mygo_version)"
		MYGO_LDFLAGS="$vflags" go run "github.com/egoist/mygo/cmd/mygo@${mygo_version}" build -platform "$app_platform" -o "$PWD/$dist/app" cmd/app
		cleanup_app
		trap - EXIT
		app_bundle="$(find "$dist/app" -maxdepth 2 -name '*.app' -type d | head -n 1)"
		for f in duet duet-helper; do
			[ -x "$app_bundle/Contents/Resources/bin/$f" ] || { echo "missing $f in $app_bundle" >&2; exit 1; }
		done
		echo "    bundled: $app_bundle/Contents/Resources/bin/{duet,duet-helper}"
		add_layered_icon "$app_bundle"
		# Sign inside out: the embedded tools (each with its own identifier,
		# not the linker's "a.out"), then the bundle; verify; then rebuild
		# the disk image from the signed bundle.
		sign_flags=(--force --sign "$sign_identity")
		if [ "$sign_identity" != "-" ]; then
			sign_flags+=(--options runtime --timestamp)
		fi
		bundle_id="$(sed -nE 's/^[[:space:]]*"identifier":[[:space:]]*"([^"]+)".*/\1/p' "$app_json" | head -n 1)"
		codesign "${sign_flags[@]}" --identifier "${bundle_id}.cli" "$app_bundle/Contents/Resources/bin/duet"
		codesign "${sign_flags[@]}" --identifier "${bundle_id}.helper" "$app_bundle/Contents/Resources/bin/duet-helper"
		codesign "${sign_flags[@]}" "$app_bundle"
		codesign --verify --deep --strict "$app_bundle"
		echo "    signed: ${sign_identity/#-/ad hoc} ($bundle_id)"
		dmg="$(find "$dist/app" -maxdepth 2 -name '*.dmg' | head -n 1)"
		if [ -n "$dmg" ]; then
			stage="$dist/.dmg-stage"
			rm -rf "$stage" && mkdir -p "$stage"
			ditto "$app_bundle" "$stage/$(basename "$app_bundle")"
			ln -s /Applications "$stage/Applications"
			rm -f "$dmg"
			hdiutil create -quiet -volname duet -srcfolder "$stage" -ov -format UDZO "$dmg"
			rm -rf "$stage"
			if [ "$sign_identity" != "-" ]; then
				codesign --force --sign "$sign_identity" --timestamp "$dmg"
			fi
			if [ -n "${NOTARY_PROFILE:-}" ]; then
				xcrun notarytool submit "$dmg" --keychain-profile "$NOTARY_PROFILE" --wait
				xcrun stapler staple "$dmg"
			fi
			echo "    disk image: $dmg"
		fi
	else
		echo "==> skipping duet.app: mygo signs and packages .app bundles on macOS only"
	fi
fi

echo "==> checksums"
(
	cd "$dist"
	files=$(find . -type f ! -name SHA256SUMS ! -path '*.app/*' | sed 's|^\./||' | sort)
	if command -v sha256sum >/dev/null; then
		sha256sum $files
	else
		shasum -a 256 $files
	fi
) >"$dist/SHA256SUMS"
cat "$dist/SHA256SUMS"
