#!/bin/sh
# Install Twinnel from a release archive on Linux (systemd).
#
#   sudo ./install.sh            twinnel -> /usr/local/bin, then `twinnel helper install`
#                                (asks before it changes anything)
#   sudo ./install.sh --yes      same, without asking
#   ./install.sh --dry-run       print every step; changes nothing, needs no root
#   sudo ./install.sh --uninstall
#                                `twinnel helper uninstall`, then remove /usr/local/bin/twinnel
#                                (settings in /var/lib/twinnel stay; add --purge to delete them)
#
# `twinnel helper install` copies twinnel-helper to /usr/local/libexec, writes
# /etc/systemd/system/twinnel-helper.service (twinnel-helper.service next to
# this script is the same unit, for reading), then enables and starts it.
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
bindir="${TWINNEL_BINDIR:-/usr/local/bin}"

mode=install
dry=0
for a in "$@"; do
	case "$a" in
	--uninstall) mode=uninstall ;;
	--dry-run) dry=1 ;;
	-h | --help) sed -n '2,15p' "$0"; exit 0 ;;
	esac
done

if [ "$mode" = uninstall ]; then
	args=""
	for a in "$@"; do [ "$a" = --uninstall ] || args="$args $a"; done
	if [ "$dry" = 1 ]; then
		# shellcheck disable=SC2086 # flags only, word splitting intended
		exec "$here/twinnel" helper uninstall $args
	fi
	[ "$(id -u)" = 0 ] || { echo "run it as root: sudo $0 $*" >&2; exit 1; }
	# shellcheck disable=SC2086
	"$here/twinnel" helper uninstall $args
	rm -f "$bindir/twinnel"
	echo "removed $bindir/twinnel"
	exit 0
fi

[ -x "$here/twinnel" ] && [ -x "$here/twinnel-helper" ] || {
	echo "twinnel and twinnel-helper must be next to this script" >&2
	exit 1
}
command -v systemctl >/dev/null 2>&1 || {
	echo "systemd (systemctl) not found: the helper needs it" >&2
	exit 1
}

if [ "$dry" = 1 ]; then
	echo "# would run: install -m 0755 $here/twinnel $bindir/twinnel"
	exec "$here/twinnel" helper install --helper-bin "$here/twinnel-helper" "$@"
fi
[ "$(id -u)" = 0 ] || { echo "run it as root: sudo $0 $*" >&2; exit 1; }

install -d -m 0755 "$bindir"
install -m 0755 "$here/twinnel" "$bindir/twinnel"
echo "installed $bindir/twinnel ($("$bindir/twinnel" version --short))"
"$bindir/twinnel" helper install --helper-bin "$here/twinnel-helper" "$@"
