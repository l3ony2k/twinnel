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
# for signing and the disk image, a macOS host. Signing is ad hoc unless
# macos.signingIdentity is set in cmd/app/mygo.json.
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
	version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
	version="${version#v}"
fi

dist=dist
rm -rf "$dist"
mkdir -p "$dist"

ldflags="-s -w -X github.com/l3ony2k/duet/core/engine.HelperVersion=${version}"

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
		mygo_version="$(go list -m -f '{{.Version}}' github.com/egoist/mygo)"
		echo "==> duet.app (mygo $mygo_version)"
		go run "github.com/egoist/mygo/cmd/mygo@${mygo_version}" build -platform darwin/arm64 -o "$PWD/$dist/app" cmd/app
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
