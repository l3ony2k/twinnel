#!/usr/bin/env bash
# Install a self-built duet.app for daily use, and update the helper if it
# changed.
#
#   scripts/install-local.sh [--dest DIR] [--backups DIR] [--app PATH] [--no-helper] [--dry-run]
#
# 1. Builds the release (scripts/build-release.sh --targets darwin/<arch>),
#    unless --app names a built duet.app.
# 2. Quits the duet running from DIR/duet.app (asks it to quit; never kills).
# 3. Moves the installed DIR/duet.app to a backup (default
#    ~/Library/Application Support/duet/backups/duet-<version>-<time>.app;
#    the newest 3 are kept) and copies the new one in its place.
# 4. Asks the new app's duet CLI whether the installed helper differs from
#    the bundled one (`duet helper upgrade --check`, read-only) and runs
#    `duet helper upgrade` only when it does: one Proceed? [y/N] and one
#    administrator password. mihomo and tailscaled are not touched.
# 5. Opens the app again in the background if it was running.
#
# --dest       install into DIR instead of /Applications (tests use a temp dir)
# --backups    keep backups in DIR
# --app        install this duet.app instead of building one
# --no-helper  skip step 4 (only print what the check says)
# --dry-run    print what would happen; change nothing (the check still runs: it only reads)
set -euo pipefail

cd "$(dirname "$0")/.."

dest=/Applications
backups="$HOME/Library/Application Support/duet/backups"
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
	-h | --help) sed -n '2,24p' "$0"; exit 0 ;;
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
	say "building duet.app (darwin/$arch)"
	if [ "$dry" = 1 ]; then
		echo "    (dry run) scripts/build-release.sh --targets darwin/$arch"
		app="dist/app/darwin-$arch/duet.app"
	else
		scripts/build-release.sh --targets "darwin/$arch" >/dev/null
		app="$(find dist/app -maxdepth 2 -name 'duet.app' -type d | head -n 1)"
	fi
fi
if [ "$dry" = 0 ]; then
	[ -d "$app" ] || { echo "no duet.app at $app" >&2; exit 1; }
	codesign --verify --deep --strict "$app" || { echo "$app: signature does not verify" >&2; exit 1; }
fi
new_version="$("$app/Contents/Resources/bin/duet-helper" --version 2>/dev/null | awk '{print $NF}' || true)"
say "new: $app (${new_version:-unknown version})"

target="$dest/duet.app"
exe="$target/Contents/MacOS/duet"

# 2. quit the running copy
was_running=0
if pgrep -f "^$exe" >/dev/null 2>&1; then
	was_running=1
	say "quitting the running duet ($target)"
	if [ "$dry" = 0 ]; then
		# Ask the app at that path to quit, by its bundle path: never another copy.
		osascript -e "tell application \"$target\" to quit" >/dev/null 2>&1 || true
		for _ in $(seq 1 20); do
			pgrep -f "^$exe" >/dev/null 2>&1 || break
			sleep 0.5
		done
		if pgrep -f "^$exe" >/dev/null 2>&1; then
			echo "duet did not quit; quit it from its menu and run this again" >&2
			exit 1
		fi
	fi
fi

# 3. back up and replace
if [ -d "$target" ]; then
	old_version="$("$target/Contents/Resources/bin/duet-helper" --version 2>/dev/null | awk '{print $NF}' || true)"
	backup="$backups/duet-${old_version:-unknown}-$(date +%Y%m%d-%H%M%S).app"
	say "backing up $target to $backup"
	run mkdir -p "$backups"
	run mv "$target" "$backup"
	if [ "$dry" = 0 ]; then
		# keep the newest $keep backups
		ls -1dt "$backups"/duet-*.app 2>/dev/null | tail -n +$((keep + 1)) | while IFS= read -r old; do
			echo "    removing old backup $old"
			rm -rf "$old"
		done
	fi
fi
say "installing $target"
run mkdir -p "$dest"
run ditto "$app" "$target"

# 4. the helper
cli="$target/Contents/Resources/bin/duet"
[ "$dry" = 1 ] && cli="$app/Contents/Resources/bin/duet"
say "checking the helper"
check="$("$cli" helper upgrade --check --json 2>&1)" || { echo "$check" >&2; exit 1; }
action="$(printf '%s' "$check" | sed -nE 's/.*"action": "([^"]+)".*/\1/p')"
"$cli" helper upgrade --check | sed 's/^/    /'
case "$action" in
upgrade | restart)
	if [ "$helper" = 1 ] && [ "$dry" = 0 ]; then
		say "updating the helper (Proceed? [y/N], then the administrator password)"
		"$cli" helper upgrade
	else
		echo "    not run (--no-helper / --dry-run): $cli helper upgrade"
	fi
	;;
none) echo "    the helper is current" ;;
not-installed) echo "    no helper installed: open duet and use 关于 → 入门 → 安装, or $cli helper install" ;;
downgrade) echo "    the installed helper is NEWER than this build; left as it is ($cli helper upgrade --force to replace it)" ;;
*) echo "    unexpected check result: $check" >&2 ;;
esac

# 5. reopen
if [ "$was_running" = 1 ]; then
	say "opening duet again (in the background)"
	run open -g "$target"
fi
say "done"
