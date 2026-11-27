#!/usr/bin/env bash
# Install a self-built Twinnel.app for daily use, and update the helper if it
# changed (or migrate the helper of the app's old name, duet).
#
#   scripts/install-local.sh [--dest DIR] [--backups DIR] [--app PATH] [--no-helper] [--dry-run]
#
# 1. Builds the release (scripts/build-release.sh --targets darwin/<arch>),
#    unless --app names a built Twinnel.app.
# 2. Quits the Twinnel running from DIR/Twinnel.app, and the app of the old
#    name (DIR/duet.app), if they run (asks them to quit; never kills).
# 3. Moves the installed DIR/Twinnel.app to a backup (default
#    ~/Library/Application Support/twinnel/backups/twinnel-<version>-<time>.app;
#    the newest 3 are kept) and copies the new one in its place. An old
#    DIR/duet.app is moved to the same folder as duet-<version>-<time>.app
#    and never deleted.
# 4. Asks the new app's twinnel CLI whether the installed helper differs from
#    the bundled one (`twinnel helper upgrade --check`, read-only) and runs
#    `twinnel helper upgrade` only when it does: one Proceed? [y/N] and one
#    administrator password. A helper of the old name is migrated in that
#    same step (its data moves to the new name; on failure it runs again
#    as before). mihomo and tailscaled are not touched.
# 5. Opens the app again in the background if it (or the old one) was
#    running.
#
# --dest       install into DIR instead of /Applications (tests use a temp dir)
# --backups    keep backups in DIR
# --app        install this Twinnel.app instead of building one
# --no-helper  skip step 4 (only print what the check says)
# --dry-run    print what would happen; change nothing (the check still runs: it only reads)
set -euo pipefail

cd "$(dirname "$0")/.."

dest=/Applications
backups="$HOME/Library/Application Support/twinnel/backups"
app=""
helper=1
dry=0
keep=3
while [ $# -gt 0 ]; do
	case "$1" in
	--dest) dest="$2"; shift 2 ;;
	--backups) backups="$2"; shift 2 ;;
	--app) app="$2"; shift 2 ;;
	--no-helper) helper=0; shift ;;
	--dry-run) dry=1; shift ;;
	-h | --help) sed -n '2,30p' "$0"; exit 0 ;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done
[ "$(uname -s)" = Darwin ] || { echo "install-local.sh is for macOS" >&2; exit 1; }

say() { printf '==> %s\n' "$*"; }
run() {
	if [ "$dry" = 1 ]; then
		printf '    (dry run) %s\n' "$*"
	else
		"$@"
	fi
}

# 1. build
if [ -z "$app" ]; then
	arch="$(uname -m)"
	[ "$arch" = x86_64 ] && arch=amd64
	say "building Twinnel.app (darwin/$arch)"
	if [ "$dry" = 1 ]; then
		echo "    (dry run) scripts/build-release.sh --targets darwin/$arch"
		app="dist/app/darwin-$arch/Twinnel.app"
	else
		scripts/build-release.sh --targets "darwin/$arch" >/dev/null
		app="$(find dist/app -maxdepth 2 -name 'Twinnel.app' -type d | head -n 1)"
	fi
fi
if [ "$dry" = 0 ]; then
	[ -d "$app" ] || { echo "no Twinnel.app at $app" >&2; exit 1; }
	codesign --verify --deep --strict "$app" || { echo "$app: signature does not verify" >&2; exit 1; }
fi
new_version="$("$app/Contents/Resources/bin/twinnel-helper" --version 2>/dev/null | awk '{print $NF}' || true)"
say "new: $app (${new_version:-unknown version})"

target="$dest/Twinnel.app"
# The app of the old name (before the rename to Twinnel).
legacy_target="$dest/duet.app"

# 2. quit the running copies (by their bundle path: never another copy)
was_running=0
quit_app() { # bundle path, executable name
	local bundle="$1" exe="$1/Contents/MacOS/$2"
	pgrep -f "^$exe" >/dev/null 2>&1 || return 0
	was_running=1
	say "quitting the running app ($bundle)"
	[ "$dry" = 1 ] && return 0
	osascript -e "tell application \"$bundle\" to quit" >/dev/null 2>&1 || true
	for _ in $(seq 1 20); do
		pgrep -f "^$exe" >/dev/null 2>&1 || return 0
		sleep 0.5
	done
	echo "$bundle did not quit; quit it from its menu and run this again" >&2
	exit 1
}
quit_app "$target" Twinnel
quit_app "$legacy_target" duet

# 3. back up and replace
stamp="$(date +%Y%m%d-%H%M%S)"
if [ -d "$target" ]; then
	old_version="$("$target/Contents/Resources/bin/twinnel-helper" --version 2>/dev/null | awk '{print $NF}' || true)"
	backup="$backups/twinnel-${old_version:-unknown}-$stamp.app"
	say "backing up $target to $backup"
	run mkdir -p "$backups"
	run mv "$target" "$backup"
	if [ "$dry" = 0 ]; then
		# keep the newest $keep backups (of Twinnel.app; the old app's copy stays)
		ls -1dt "$backups"/twinnel-*.app 2>/dev/null | tail -n +$((keep + 1)) | while IFS= read -r old; do
			echo "    removing old backup $old"
			rm -rf "$old"
		done
	fi
fi
legacy_moved=""
if [ -d "$legacy_target" ]; then
	legacy_version="$("$legacy_target/Contents/Resources/bin/duet-helper" --version 2>/dev/null | awk '{print $NF}' || true)"
	legacy_moved="$backups/duet-${legacy_version:-unknown}-$stamp.app"
	say "moving the app of the old name $legacy_target to $legacy_moved (kept; delete it yourself once Twinnel works)"
	run mkdir -p "$backups"
	run mv "$legacy_target" "$legacy_moved"
fi
say "installing $target"
run mkdir -p "$dest"
run ditto "$app" "$target"

# 4. the helper
cli="$target/Contents/Resources/bin/twinnel"
[ "$dry" = 1 ] && cli="$app/Contents/Resources/bin/twinnel"
say "checking the helper"
check="$("$cli" helper upgrade --check --json 2>&1)" || { echo "$check" >&2; exit 1; }
action="$(printf '%s' "$check" | sed -nE 's/.*"action": "([^"]+)".*/\1/p')"
"$cli" helper upgrade --check | sed 's/^/    /'
migrated=0
case "$action" in
upgrade | restart | migrate)
	if [ "$helper" = 1 ] && [ "$dry" = 0 ]; then
		if [ "$action" = migrate ]; then
			say "migrating the helper of the old name (Proceed? [y/N], then the administrator password)"
		else
			say "updating the helper (Proceed? [y/N], then the administrator password)"
		fi
		"$cli" helper upgrade
		[ "$action" = migrate ] && migrated=1
	else
		echo "    not run (--no-helper / --dry-run): $cli helper upgrade"
	fi
	;;
none) echo "    the helper is current" ;;
not-installed) echo "    no helper installed: open Twinnel and use 关于 → 入门 → 安装, or $cli helper install" ;;
downgrade) echo "    the installed helper is NEWER than this build; left as it is ($cli helper upgrade --force to replace it)" ;;
*) echo "    unexpected check result: $check" >&2 ;;
esac

# 5. reopen
if [ "$was_running" = 1 ]; then
	say "opening Twinnel (in the background)"
	run open -g "$target"
fi

if [ -n "$legacy_moved" ] || [ "$migrated" = 1 ]; then
	say "after the rename, check by hand:"
	echo "    - System Settings → General → Login Items: turn on Twinnel if duet was there, and remove duet"
	echo "      (or Twinnel → 设置 → 通用 → 登录时打开)."
	echo "    - Notifications: allow Twinnel when macOS asks (it is a new app to macOS)."
	echo "    - An MCP client set up with \`duet mcp\`: claude mcp remove duet; claude mcp add twinnel -- twinnel mcp"
	[ -n "$legacy_moved" ] && echo "    - The old app is kept at $legacy_moved"
	[ "$migrated" = 1 ] && echo "    - The old helper's data is kept as /Library/Application Support/duet.migrated-<time> (sudo rm -rf it once Twinnel works)"
fi
say "done"
