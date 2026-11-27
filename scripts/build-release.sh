#!/usr/bin/env bash
# Build release artifacts into dist/ (gitignored):
#   dist/twinnel_<version>_<os>_<arch>.tar.gz   the twinnel CLI and twinnel-helper
#                                             (Linux: plus install.sh and the systemd unit)
#   dist/Twinnel_<version>_macos_<arch>.dmg     Twinnel.app in a disk image  } macOS
#   dist/Twinnel_<version>_macos_<arch>.zip     Twinnel.app zipped (ditto)    } hosts
#   dist/app/<platform>/Twinnel.app            the signed bundle itself      } only
#   dist/SHA256SUMS
#
# Usage: scripts/build-release.sh [-v VERSION] [--no-app] [--targets "darwin/arm64 linux/amd64"]
#                                 [--app-platform darwin/arm64|darwin/amd64|darwin/universal]
#
# --targets "" builds no CLI archives (the app only). --app-platform picks the
# app's architecture (default darwin/arm64); darwin/universal makes one app
# for Apple silicon and Intel, with universal embedded tools (no Xcode needed).
#
# Pure Go, no cgo: every target cross-compiles from any host. The .app needs
# `mygo build` (run through `go run`, pinned to the version in go.mod) and,
# for signing and the disk image, a macOS host.
#
# Signing: the embedded twinnel and twinnel-helper, then the .app, are signed with
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
# (assets/appicon/twinnel.icon → Assets.car); TWINNEL_REQUIRE_LAYERED_ICON=1 fails
# the build without it (docs/release.md).
set -euo pipefail

cd "$(dirname "$0")/.."

version=""
build_app=1
app_platform="darwin/arm64"
targets="darwin/arm64 darwin/amd64 linux/amd64 linux/arm64"
while [ $# -gt 0 ]; do
	case "$1" in
	-v | --version) version="$2"; shift 2 ;;
	--no-app) build_app=0; shift ;;
	--targets) targets="$2"; shift 2 ;;
	--app-platform) app_platform="$2"; shift 2 ;;
	-h | --help) sed -n '2,19p' "$0"; exit 0 ;;
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

# The macOS 26 layered icon (assets/appicon/twinnel.icon, Icon Composer) needs
# actool from Xcode 26. With it, the .icon is compiled to Assets.car in
# Contents/Resources and named by CFBundleIconName; AppIcon.icns stays as
# CFBundleIconFile for older macOS. Without it the app keeps the .icns only
# and the build warns; TWINNEL_REQUIRE_LAYERED_ICON=1 (release CI) makes that
# an error.
layered_icon=assets/appicon/twinnel.icon
add_layered_icon() {
	local app="$1" out msg
	if ! xcrun --find actool >/dev/null 2>&1; then
		if [ "${TWINNEL_REQUIRE_LAYERED_ICON:-0}" = 1 ]; then
			echo "error: TWINNEL_REQUIRE_LAYERED_ICON=1 but actool (Xcode 26) is not available" >&2
			exit 1
		fi
		echo "    warning: no actool (Xcode 26): Twinnel.app gets the .icns only, no layered macOS 26 icon" >&2
		return 0
	fi
	out="$dist/.actool"
	rm -rf "$out" && mkdir -p "$out"
	# --minimum-deployment-target 26.0: the catalog carries only the layered
	# icon; macOS 15 and earlier take CFBundleIconFile (our drawn .icns).
	if ! msg="$(xcrun actool "$layered_icon" --compile "$out" --output-format human-readable-text \
		--platform macosx --target-device mac --minimum-deployment-target 26.0 \
		--app-icon twinnel --output-partial-info-plist "$out/partial.plist" 2>&1)" || [ ! -f "$out/Assets.car" ]; then
		echo "$msg" >&2
		if [ "${TWINNEL_REQUIRE_LAYERED_ICON:-0}" = 1 ]; then
			echo "error: actool could not compile $layered_icon" >&2
			exit 1
		fi
		echo "    warning: actool could not compile $layered_icon: Twinnel.app gets the .icns only" >&2
		rm -rf "$out"
		return 0
	fi
	cp "$out/Assets.car" "$app/Contents/Resources/Assets.car"
	plutil -replace CFBundleIconName -string twinnel "$app/Contents/Info.plist"
	rm -rf "$out"
	echo "    layered icon: Assets.car (CFBundleIconName twinnel), .icns kept for macOS 15 and earlier"
}

dist=dist
rm -rf "$dist"
mkdir -p "$dist"

# core/version.Version is twinnel's version (CLI, app); the helper reports
# engine.HelperVersion.
vflags="-X github.com/l3ony2k/twinnel/core/version.Version=${version} -X github.com/l3ony2k/twinnel/core/engine.HelperVersion=${version}"
ldflags="-s -w ${vflags}"

for t in $targets; do
	os="${t%/*}"
	arch="${t#*/}"
	name="twinnel_${version}_${os}_${arch}"
	out="$dist/$name"
	mkdir -p "$out"
	echo "==> $os/$arch"
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$out/twinnel" ./cmd/cli
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$out/twinnel-helper" ./cmd/helper
	cp LICENSE README.md "$out/" 2>/dev/null || true
	cp docs/install.md "$out/INSTALL.md"
	if [ "$os" = linux ]; then
		# install.sh puts twinnel in /usr/local/bin and runs `twinnel helper
		# install`, which writes the unit itself; the .service file is the
		# same unit for reading (its --group is the one picked on the host).
		cp packaging/linux/install.sh "$out/install.sh"
		cp packaging/linux/twinnel-helper.service "$out/twinnel-helper.service"
		chmod 0755 "$out/install.sh"
	fi
	tar -C "$dist" -czf "$dist/$name.tar.gz" "$name"
	rm -rf "$out"
done

if [ "$build_app" = 1 ]; then
	if [ "$(uname -s)" = Darwin ]; then
		# Twinnel.app ships the CLI and the helper in Contents/Resources/bin, so
		# the first-run 安装后台助手 button works from a packaged app (the app
		# runs bin/twinnel helper install --helper-bin bin/twinnel-helper).
		# Contents/MacOS/Twinnel is the app itself. mygo copies the project's
		# resources/<goos>-<goarch>/ into the bundle and signs the binaries.
		app_arch="${app_platform#*/}"
		case "$app_platform" in
		darwin/arm64 | darwin/amd64) res_arches="$app_arch" ;;
		darwin/universal) res_arches="arm64 amd64" ;; # mygo lipos darwin-arm64 + darwin-amd64
		*) echo "unsupported --app-platform $app_platform" >&2; exit 2 ;;
		esac
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
		for a in $res_arches; do
			mkdir -p "$res/darwin-$a/bin"
			for cmd in cli:twinnel helper:twinnel-helper; do
				CGO_ENABLED=0 GOOS=darwin GOARCH="$a" go build -trimpath -ldflags "$ldflags" \
					-o "$res/darwin-$a/bin/${cmd#*:}" "./cmd/${cmd%:*}"
			done
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
		echo "==> Twinnel.app ${bundle_version} (mygo $mygo_version)"
		MYGO_LDFLAGS="$vflags" go run "github.com/egoist/mygo/cmd/mygo@${mygo_version}" build -platform "$app_platform" -o "$PWD/$dist/app" cmd/app
		cleanup_app
		trap - EXIT
		app_bundle="$(find "$dist/app" -maxdepth 2 -name '*.app' -type d | head -n 1)"
		for f in twinnel twinnel-helper; do
			[ -x "$app_bundle/Contents/Resources/bin/$f" ] || { echo "missing $f in $app_bundle" >&2; exit 1; }
		done
		echo "    bundled: $app_bundle/Contents/Resources/bin/{twinnel,twinnel-helper}"
		add_layered_icon "$app_bundle"
		# Sign inside out: the embedded tools (each with its own identifier,
		# not the linker's "a.out"), then the bundle; verify; then rebuild
		# the disk image from the signed bundle.
		sign_flags=(--force --sign "$sign_identity")
		if [ "$sign_identity" != "-" ]; then
			sign_flags+=(--options runtime --timestamp)
		fi
		bundle_id="$(sed -nE 's/^[[:space:]]*"identifier":[[:space:]]*"([^"]+)".*/\1/p' "$app_json" | head -n 1)"
		codesign "${sign_flags[@]}" --identifier "${bundle_id}.cli" "$app_bundle/Contents/Resources/bin/twinnel"
		codesign "${sign_flags[@]}" --identifier "${bundle_id}.helper" "$app_bundle/Contents/Resources/bin/twinnel-helper"
		codesign "${sign_flags[@]}" "$app_bundle"
		codesign --verify --deep --strict "$app_bundle"
		echo "    signed: ${sign_identity/#-/ad hoc} ($bundle_id)"
		app_name="Twinnel_${version}_macos_${app_arch}"
		# The .zip keeps the signature and the bundle's symlinks (ditto, as
		# Finder's Compress does).
		ditto -c -k --keepParent "$app_bundle" "$dist/$app_name.zip"
		echo "    zip: $dist/$app_name.zip"
		dmg="$(find "$dist/app" -maxdepth 2 -name '*.dmg' | head -n 1)"
		if [ -n "$dmg" ]; then
			stage="$dist/.dmg-stage"
			rm -rf "$stage" && mkdir -p "$stage"
			ditto "$app_bundle" "$stage/$(basename "$app_bundle")"
			ln -s /Applications "$stage/Applications"
			rm -f "$dmg"
			hdiutil create -quiet -volname twinnel -srcfolder "$stage" -ov -format UDZO "$dmg"
			rm -rf "$stage"
			if [ "$sign_identity" != "-" ]; then
				codesign --force --sign "$sign_identity" --timestamp "$dmg"
			fi
			if [ -n "${NOTARY_PROFILE:-}" ]; then
				xcrun notarytool submit "$dmg" --keychain-profile "$NOTARY_PROFILE" --wait
				xcrun stapler staple "$dmg"
			fi
			mv "$dmg" "$dist/$app_name.dmg"
			echo "    disk image: $dist/$app_name.dmg"
		fi
	else
		echo "==> skipping Twinnel.app: mygo signs and packages .app bundles on macOS only"
	fi
fi

echo "==> checksums"
(
	cd "$dist"
	files=$(find . -maxdepth 1 -type f ! -name SHA256SUMS ! -name '.*' | sed 's|^\./||' | sort)
	[ -n "$files" ] || exit 0
	# shellcheck disable=SC2086 # release file names have no spaces
	if command -v sha256sum >/dev/null; then
		sha256sum $files
	else
		shasum -a 256 $files
	fi
) >"$dist/SHA256SUMS"
cat "$dist/SHA256SUMS"
