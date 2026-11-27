#!/usr/bin/env bash
# Package the Linux GUI that `mygo build -platform linux/<arch>` compiled
# (scripts/build-release.sh runs this; you rarely call it yourself).
#
#   scripts/package-linux-app.sh app -v VERSION -a ARCH -i MYGO_DIR -o DIST [--appimage]
#       DIST/Twinnel_<v>_linux_<arch>_app.tar.gz  the app, twinnel, twinnel-helper,
#                                             desktop entry, icons, install.sh, uninstall.sh
#       DIST/Twinnel_<v>_linux_<arch>.AppImage with --appimage (needs mksquashfs)
#   scripts/package-linux-app.sh pkgbuild -v VERSION -o DIST [-r OWNER/REPO]
#       DIST/PKGBUILD, DIST/twinnel-bin.install   Arch Linux package of the release
#                                             archives in DIST (sha256 of each arch present)
#
# MYGO_DIR is mygo's output for one target: the app, named after mygo's slug
# (twinnel), and bin/ next to it. The app is renamed Twinnel here, apart
# from the twinnel command line tool; its window class (StartupWMClass) and
# Wayland app id follow the executable's name. The
# AppImage runtime is AppImage/type2-runtime, pinned by tag and SHA-256
# below; $TWINNEL_APPIMAGE_RUNTIME names a local copy instead of downloading
# (still checked).
set -euo pipefail

cd "$(dirname "$0")/.."
pkg=packaging/linux

runtime_tag=20251108
runtime_sha_x86_64=2fca8b443c92510f1483a883f60061ad09b46b978b2631c807cd873a47ec260d
runtime_sha_aarch64=00cbdfcf917cc6c0ff6d3347d59e0ca1f7f45a6df1a428a0d6d8a78664d87444

die() { echo "package-linux-app: $*" >&2; exit 1; }

sha256() {
	if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

cmd="${1:-}"
[ $# -gt 0 ] && shift
version="" arch="" in="" out="" appimage=0 repo="${TWINNEL_REPO:-l3ony2k/twinnel}"
while [ $# -gt 0 ]; do
	case "$1" in
	-v) version="$2"; shift 2 ;;
	-a) arch="$2"; shift 2 ;;
	-i) in="$2"; shift 2 ;;
	-o) out="$2"; shift 2 ;;
	-r) repo="$2"; shift 2 ;;
	--appimage) appimage=1; shift ;;
	*) die "unknown argument: $1" ;;
	esac
done
[ -n "$version" ] && [ -n "$out" ] || die "usage: see the top of $0"

# The files of the app outside its executables, laid out under share/ as
# they install under a prefix (install.sh copies share/ as it is).
stage_share() {
	local share="$1" s
	install -d "$share/applications" "$share/icons/hicolor"
	install -m 0644 "$pkg/twinnel.desktop" "$share/applications/twinnel.desktop"
	for s in assets/appicon/linux/hicolor/*/apps; do
		s="${s#assets/appicon/linux/hicolor/}"
		install -d "$share/icons/hicolor/$s"
		install -m 0644 assets/appicon/linux/hicolor/"$s"/* "$share/icons/hicolor/$s/"
	done
	# The tray's state glyphs, for trays that take icon names from the theme.
	install -d "$share/icons/hicolor/symbolic/apps"
	install -m 0644 assets/icons/tray/linux/*-symbolic.svg "$share/icons/hicolor/symbolic/apps/"
}

appimage_runtime() {
	local a="$1" want f
	case "$a" in
	x86_64) want="$runtime_sha_x86_64" ;;
	aarch64) want="$runtime_sha_aarch64" ;;
	esac
	if [ -n "${TWINNEL_APPIMAGE_RUNTIME:-}" ]; then
		f="$TWINNEL_APPIMAGE_RUNTIME"
	else
		f="${XDG_CACHE_HOME:-$HOME/.cache}/twinnel-build/appimage-runtime-$runtime_tag-$a"
		if [ ! -f "$f" ] || [ "$(sha256 "$f")" != "$want" ]; then
			mkdir -p "$(dirname "$f")"
			curl -fsSL --retry 3 -o "$f.part" \
				"https://github.com/AppImage/type2-runtime/releases/download/$runtime_tag/runtime-$a"
			mv "$f.part" "$f"
		fi
	fi
	[ "$(sha256 "$f")" = "$want" ] || die "AppImage runtime $f: SHA-256 mismatch (want $want)"
	printf '%s\n' "$f"
}

package_app() {
	[ -n "$arch" ] && [ -n "$in" ] || die "app: -a ARCH and -i MYGO_DIR are required"
	[ -x "$in/twinnel" ] && [ -f "$in/twinnel.desktop" ] || die "$in/twinnel: not found (mygo build output)"
	local b
	for b in twinnel twinnel-helper; do
		[ -x "$in/bin/$b" ] || die "$in/bin/$b: not found"
	done

	# _app: apart from the CLI archive twinnel_<v>_linux_<arch>.tar.gz on
	# case-insensitive file systems.
	local name="Twinnel_${version}_linux_${arch}_app"
	local dir="$out/$name"
	rm -rf "$dir"
	install -d "$dir/bin"
	install -m 0755 "$in/twinnel" "$dir/Twinnel"
	install -m 0755 "$in/bin/twinnel" "$in/bin/twinnel-helper" "$dir/bin/"
	stage_share "$dir/share"
	install -m 0755 "$pkg/app/install.sh" "$dir/install.sh"
	install -m 0755 "$pkg/app/uninstall.sh" "$dir/uninstall.sh"
	install -m 0644 "$pkg/twinnel-helper.service" "$dir/twinnel-helper.service"
	install -m 0644 LICENSE README.md "$dir/"
	install -m 0644 docs/install.md "$dir/INSTALL.md"
	printf '%s\n' "$version" >"$dir/VERSION"
	COPYFILE_DISABLE=1 tar -C "$out" -czf "$out/$name.tar.gz" "$name"
	echo "    archive: $out/$name.tar.gz"

	if [ "$appimage" = 1 ]; then
		command -v mksquashfs >/dev/null || die "--appimage needs mksquashfs (squashfs-tools)"
		local ai_arch
		case "$arch" in
		amd64) ai_arch=x86_64 ;;
		arm64) ai_arch=aarch64 ;;
		*) die "no AppImage runtime for $arch" ;;
		esac
		local runtime appdir="$out/.appdir-$arch" lib
		runtime="$(appimage_runtime "$ai_arch")"
		rm -rf "$appdir"
		lib="$appdir/usr/lib/twinnel"
		install -d "$lib/bin"
		install -m 0755 "$dir/Twinnel" "$lib/Twinnel"
		install -m 0755 "$dir/bin/twinnel" "$dir/bin/twinnel-helper" "$lib/bin/"
		install -m 0644 "$dir/VERSION" "$lib/VERSION"
		stage_share "$appdir/usr/share"
		install -m 0755 "$pkg/appimage/AppRun" "$appdir/AppRun"
		# The AppImage spec wants the desktop entry and its icon at the top.
		sed -e 's|^Exec=.*|Exec=Twinnel|' "$pkg/twinnel.desktop" >"$appdir/twinnel.desktop"
		install -m 0644 assets/appicon/linux/hicolor/256x256/apps/twinnel.png "$appdir/twinnel.png"
		ln -s twinnel.png "$appdir/.DirIcon"
		rm -f "$out/.squashfs-$arch"
		mksquashfs "$appdir" "$out/.squashfs-$arch" -root-owned -noappend -comp zstd -quiet >/dev/null
		local image="$out/Twinnel_${version}_linux_${arch}.AppImage"
		cat "$runtime" "$out/.squashfs-$arch" >"$image"
		chmod 0755 "$image"
		rm -rf "$appdir" "$out/.squashfs-$arch"
		echo "    AppImage: $image (runtime $runtime_tag)"
	fi
	rm -rf "$dir"
}

# pacman's pkgver holds no hyphen: 1.2.0-beta.1 -> 1.2.0.beta.1 (which vercmp
# sorts before 1.2.0), 0.0.0-12-gabc -> 0.0.0.12.gabc, + -> _.
package_pkgbuild() {
	local pkgver sx sa
	pkgver="$(printf '%s' "$version" | tr -- '-+' '._')"
	[ -f "$out/Twinnel_${version}_linux_amd64_app.tar.gz" ] && sx="$(sha256 "$out/Twinnel_${version}_linux_amd64_app.tar.gz")"
	[ -f "$out/Twinnel_${version}_linux_arm64_app.tar.gz" ] && sa="$(sha256 "$out/Twinnel_${version}_linux_arm64_app.tar.gz")"
	[ -n "${sx:-}${sa:-}" ] || die "pkgbuild: no Twinnel_${version}_linux_*_app.tar.gz in $out"
	local arches=""
	[ -n "${sx:-}" ] && arches="$arches 'x86_64'"
	[ -n "${sa:-}" ] && arches="$arches 'aarch64'"
	sed -e "s|@VERSION@|$version|g" \
		-e "s|@PKGVER@|$pkgver|g" \
		-e "s|@REPO@|$repo|g" \
		-e "s|@ARCHES@|${arches# }|g" \
		-e "s|@SHA_X86_64@|${sx:-SKIP}|g" \
		-e "s|@SHA_AARCH64@|${sa:-SKIP}|g" \
		"$pkg/arch/PKGBUILD.in" >"$out/PKGBUILD"
	install -m 0644 "$pkg/arch/twinnel-bin.install" "$out/twinnel-bin.install"
	echo "    PKGBUILD: $out/PKGBUILD (pkgver $pkgver)"
}

case "$cmd" in
app) package_app ;;
pkgbuild) package_pkgbuild ;;
*) die "usage: $0 app|pkgbuild ..." ;;
esac
