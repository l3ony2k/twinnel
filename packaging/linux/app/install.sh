#!/bin/sh
# Install Twinnel (the app, the twinnel command and the helper) from the
# Linux release archive.
#
#   ./install.sh               for you: ~/.local (no root for the files), then
#                              the helper, which asks for an administrator
#                              password once (pkexec, else sudo)
#   sudo ./install.sh          for every user: /usr/local
#   ./install.sh --system      same, asks for the password for the files too
#   ./install.sh --no-helper   the app only; install the helper later from the app
#   ./install.sh --yes         do not ask before installing the helper
#   ./install.sh --dry-run     print every step; change nothing
#
# Files: <prefix>/lib/twinnel/{Twinnel,bin/twinnel,bin/twinnel-helper},
# <prefix>/bin/twinnel (a link), the desktop entry and icons under
# <prefix>/share. The helper itself (twinnel helper install) goes to
# /usr/local/libexec/twinnel-helper and the systemd unit twinnel-helper.service,
# whichever prefix the app has. Run it again to update. Remove with ./uninstall.sh.
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
scope=user
helper=1
yes=0
dry=0
for a in "$@"; do
	case "$a" in
	--system) scope=system ;;
	--user) scope=user ;;
	--no-helper) helper=0 ;;
	--yes | -y) yes=1 ;;
	--dry-run) dry=1 ;;
	-h | --help) sed -n '2,19p' "$0"; exit 0 ;;
	*) echo "unknown argument: $a (see --help)" >&2; exit 2 ;;
	esac
done
[ "$(id -u)" = 0 ] && scope=system

for f in Twinnel bin/twinnel bin/twinnel-helper share/applications/twinnel.desktop; do
	[ -e "$here/$f" ] || { echo "$f must be next to this script (run it from the extracted archive)" >&2; exit 1; }
done

if [ "$scope" = system ]; then
	prefix=/usr/local
	share=/usr/local/share
else
	prefix="$HOME/.local"
	share="${XDG_DATA_HOME:-$HOME/.local/share}"
fi
lib="$prefix/lib/twinnel"
bindir="$prefix/bin"

# as_root runs a command as root: directly when we are root, else through
# pkexec (a graphical password dialog, or a prompt here), else sudo.
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

# run prints a step; in a dry run that is all it does.
run() {
	echo "+ $*"
	[ "$dry" = 1 ] || "$@"
}

if [ "$scope" = system ] && [ "$(id -u)" != 0 ] && [ "$dry" = 0 ]; then
	# Re-run this script as root, once, for the files and the helper.
	echo "installing for every user under $prefix: root is needed"
	set -- --system
	if [ "$helper" = 0 ]; then set -- "$@" --no-helper; fi
	if [ "$yes" = 1 ]; then set -- "$@" --yes; fi
	as_root "$here/install.sh" "$@"
	exit 0
fi

echo "Twinnel $(cat "$here/VERSION" 2>/dev/null || echo) -> $lib"

# The app directory is replaced whole (a running Twinnel keeps its old,
# deleted files open; the next start runs the new ones).
run install -d -m 0755 "$prefix/lib" "$bindir"
run rm -rf "$lib.new"
run install -d -m 0755 "$lib.new/bin"
run install -m 0755 "$here/Twinnel" "$lib.new/Twinnel"
run install -m 0755 "$here/bin/twinnel" "$here/bin/twinnel-helper" "$lib.new/bin/"
if [ -f "$here/VERSION" ]; then run install -m 0644 "$here/VERSION" "$lib.new/VERSION"; fi
run rm -rf "$lib"
run mv "$lib.new" "$lib"

# twinnel on PATH, unless another program of that name is there (a link of
# ours, or one into an older copy of the app, is replaced).
link="$bindir/twinnel"
if [ -e "$link" ] && [ ! -L "$link" ]; then
	echo "# $link exists and is not ours: left alone (the app uses its own copy)"
else
	run ln -sfn "$lib/bin/twinnel" "$link"
fi

# Desktop entry (runs this copy) and icons.
run install -d -m 0755 "$share/applications"
echo "+ write $share/applications/twinnel.desktop (Exec=$lib/Twinnel)"
if [ "$dry" = 0 ]; then
	sed -e "s|^Exec=.*|Exec=$lib/Twinnel|" "$here/share/applications/twinnel.desktop" >"$share/applications/twinnel.desktop"
	chmod 0644 "$share/applications/twinnel.desktop"
fi
(cd "$here/share/icons" && find hicolor -type f) | while read -r f; do
	run install -d -m 0755 "$share/icons/${f%/*}"
	run install -m 0644 "$here/share/icons/$f" "$share/icons/$f"
done
if [ "$dry" = 0 ]; then
	command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database -q "$share/applications" 2>/dev/null || true
	command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q -t -f "$share/icons/hicolor" 2>/dev/null || true
	touch "$share/icons/hicolor" 2>/dev/null || true
fi

case ":$PATH:" in
*":$bindir:"*) ;;
*) echo "# note: $bindir is not on your PATH; open Twinnel from the applications menu, or add it" ;;
esac

[ "$helper" = 1 ] || { echo "done. Open Twinnel from the applications menu; it offers to install the helper."; exit 0; }

# The helper: a root service (systemd) that applies every change Twinnel makes.
command -v systemctl >/dev/null 2>&1 || {
	echo "done, without the helper: systemd (systemctl) not found, and the helper needs it" >&2
	exit 0
}
cli="$lib/bin/twinnel"
[ "$dry" = 1 ] && cli="$here/bin/twinnel"
hbin="$lib/bin/twinnel-helper"
op=install
if [ -f /etc/systemd/system/twinnel-helper.service ]; then
	op=upgrade # replaces an older helper; does nothing when it is this one
fi
if [ "$dry" = 1 ]; then
	echo "+ (as root) $lib/bin/twinnel helper $op --helper-bin $hbin --yes; its plan:"
	"$cli" helper "$op" --dry-run --helper-bin "$here/bin/twinnel-helper" || true
	exit 0
fi
if [ "$yes" = 0 ]; then
	"$cli" helper "$op" --dry-run --helper-bin "$hbin" || true
	if [ -t 0 ]; then
		printf 'Install the helper now (asks for an administrator password)? [Y/n] '
		read -r ans || ans=n
		case "$ans" in
		[nN]*) echo "skipped; install it later from the app, or: $0 --yes"; exit 0 ;;
		esac
	fi
fi
as_root "$cli" helper "$op" --helper-bin "$hbin" --yes
echo "done. Open Twinnel from the applications menu."
