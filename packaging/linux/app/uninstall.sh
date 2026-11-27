#!/bin/sh
# Remove Twinnel installed by install.sh.
#
#   ./uninstall.sh               your copy (~/.local), and the helper
#   sudo ./uninstall.sh          the copy for every user (/usr/local), and the helper
#   ./uninstall.sh --system      same, asks for the password
#   ./uninstall.sh --keep-helper leave the helper service installed
#   ./uninstall.sh --purge       also delete the helper's data (/var/lib/twinnel)
#   ./uninstall.sh --dry-run     print every step; change nothing
#
# The helper goes first (twinnel helper uninstall: it puts back what Twinnel
# changed, then removes its service), while its twinnel is still here.
# Your own settings (~/.config/Twinnel) stay.
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
scope=user
helper=1
purge=""
dry=0
for a in "$@"; do
	case "$a" in
	--system) scope=system ;;
	--user) scope=user ;;
	--keep-helper) helper=0 ;;
	--purge) purge=--purge ;;
	--dry-run) dry=1 ;;
	-h | --help) sed -n '2,13p' "$0"; exit 0 ;;
	*) echo "unknown argument: $a (see --help)" >&2; exit 2 ;;
	esac
done
[ "$(id -u)" = 0 ] && scope=system

if [ "$scope" = system ]; then
	prefix=/usr/local
	share=/usr/local/share
else
	prefix="$HOME/.local"
	share="${XDG_DATA_HOME:-$HOME/.local/share}"
fi
lib="$prefix/lib/twinnel"
bindir="$prefix/bin"

as_root() {
	if [ "$(id -u)" = 0 ]; then
		"$@"
	elif command -v pkexec >/dev/null 2>&1 && { [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ] || ! command -v sudo >/dev/null 2>&1; }; then
		pkexec "$@"
	elif command -v sudo >/dev/null 2>&1; then
		sudo "$@"
	else
		echo "need root for: $*  (neither pkexec nor sudo found)" >&2
		return 1
	fi
}

run() {
	echo "+ $*"
	[ "$dry" = 1 ] || "$@"
}

if [ "$scope" = system ] && [ "$(id -u)" != 0 ] && [ "$dry" = 0 ]; then
	set -- --system
	if [ "$helper" = 0 ]; then set -- "$@" --keep-helper; fi
	if [ -n "$purge" ]; then set -- "$@" --purge; fi
	as_root "$here/uninstall.sh" "$@"
	exit 0
fi

# The helper, with the twinnel of this copy (else the one next to this
# script, else any on PATH).
if [ "$helper" = 1 ] && [ -f /etc/systemd/system/twinnel-helper.service ]; then
	cli=""
	for c in "$lib/bin/twinnel" "$here/bin/twinnel"; do
		[ -x "$c" ] && { cli="$c"; break; }
	done
	[ -n "$cli" ] || cli="$(command -v twinnel || true)"
	if [ -z "$cli" ]; then
		echo "no twinnel found to remove the helper with; keeping it" >&2
	elif [ "$dry" = 1 ]; then
		echo "+ (as root) $cli helper uninstall --yes $purge; its plan:"
		# shellcheck disable=SC2086 # $purge is one flag or nothing
		"$cli" helper uninstall --dry-run $purge || true
	else
		# shellcheck disable=SC2086
		as_root "$cli" helper uninstall --yes $purge
	fi
fi

link="$bindir/twinnel"
if [ -L "$link" ]; then
	case "$(readlink "$link")" in
	"$lib"/*) run rm -f "$link" ;;
	esac
fi
run rm -f "$share/applications/twinnel.desktop"
if [ -d "$here/share/icons/hicolor" ]; then
	(cd "$here/share/icons" && find hicolor -type f) | while read -r f; do
		if [ -e "$share/icons/$f" ]; then run rm -f "$share/icons/$f"; fi
	done
else
	for f in "$share"/icons/hicolor/*/apps/twinnel.png "$share"/icons/hicolor/*/apps/twinnel.svg "$share"/icons/hicolor/*/apps/twinnel*-symbolic.svg; do
		if [ -e "$f" ]; then run rm -f "$f"; fi
	done
fi
run rm -rf "$lib"
if [ "$dry" = 0 ]; then
	command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database -q "$share/applications" 2>/dev/null || true
	command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q -t -f "$share/icons/hicolor" 2>/dev/null || true
fi
echo "removed Twinnel from $prefix"
