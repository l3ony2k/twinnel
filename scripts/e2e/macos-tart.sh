#!/usr/bin/env bash
# Fresh-machine test of Twinnel on macOS 26 in a throwaway tart VM
# (cirruslabs/tart, Apple silicon). Nothing here touches the host's network,
# helper, mihomo or Tailscale: every step acts on the VM, over ssh to the VM's
# own address.
#
# Usage: scripts/e2e/macos-tart.sh <command> [options]
#
#   create              clone a clean macOS 26 image into a new VM ($VM)
#   up [--headless]     boot the VM (a window, needed for the GUI steps;
#                       --headless runs it without one) and wait for ssh
#   copy APP|ZIP|DMG    copy a built Twinnel.app (or a release .zip / .dmg)
#                       into the VM's /Applications
#   fetch TAG           download the release's macOS .zip with gh, then copy it
#   manual              print the GUI steps to do by hand in the VM window
#   check               run the fresh-setup checklist in the VM and print the
#                       pass/fail table (exit 1 when any step fails)
#   collect             copy the checklist's raw JSON from the VM to $WORK/out
#   ssh [CMD...]        open a shell in the VM (or run CMD there)
#   destroy             stop and delete the VM, and delete $WORK
#   all APP|ZIP|DMG     create, up, copy, manual (waits for Enter), check
#
# Environment:
#   VM                  VM name (default twinnel-e2e)
#   TART_IMAGE          image (default ghcr.io/cirruslabs/macos-tahoe-vanilla:latest,
#                       macOS 26 with only the admin user and ssh; the -base
#                       image adds Homebrew, which a fresh machine does not have)
#   VM_USER, VM_PASS    VM login (default admin / admin, the cirruslabs images')
#   WORK                scratch dir on the host (default $TMPDIR/twinnel-e2e-$VM)
#   GH_REPO             repository for fetch (default l3ony2k/twinnel)
#
# ssh logs in with sshpass when it is installed, else asks for the password
# (admin) each time; `ssh-copy-id` to the VM once avoids both.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
VM="${VM:-twinnel-e2e}"
TART_IMAGE="${TART_IMAGE:-ghcr.io/cirruslabs/macos-tahoe-vanilla:latest}"
VM_USER="${VM_USER:-admin}"
VM_PASS="${VM_PASS:-admin}"
GH_REPO="${GH_REPO:-l3ony2k/twinnel}"
tmp="${TMPDIR:-/tmp}"
WORK="${WORK:-${tmp%/}/twinnel-e2e-$VM}"

die() { echo "macos-tart: $*" >&2; exit 1; }
say() { echo "==> $*"; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 not found${2:+ ($2)}"; }

case "$WORK" in
/*) ;;
*) die "WORK must be an absolute path: $WORK" ;;
esac

ssh_opts=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=5)

vm_ip() {
	tart ip --wait 120 "$VM" 2>/dev/null || die "VM $VM has no IP (is it running? try: $0 up)"
}

with_pass() {
	if command -v sshpass >/dev/null 2>&1; then
		SSHPASS="$VM_PASS" sshpass -e "$@"
	else
		"$@"
	fi
}

vm_ssh() {
	local ip
	ip="$(vm_ip)"
	with_pass ssh "${ssh_opts[@]}" "$VM_USER@$ip" "$@"
}

vm_scp_to() { # local remote
	local ip
	ip="$(vm_ip)"
	with_pass scp "${ssh_opts[@]}" -q "$1" "$VM_USER@$ip:$2"
}

vm_scp_from() { # remote local
	local ip
	ip="$(vm_ip)"
	with_pass scp "${ssh_opts[@]}" -q -r "$VM_USER@$ip:$1" "$2"
}

# `tart list` prints: Source Name Disk Size [Accessed…] State
vm_state() { tart list 2>/dev/null | awk -v n="$VM" '$2 == n { print $NF }'; }
vm_exists() { [ -n "$(vm_state)" ]; }

cmd_create() {
	need tart "brew install cirruslabs/cli/tart"
	[ "$(uname -s)/$(uname -m)" = Darwin/arm64 ] || die "tart needs an Apple silicon Mac"
	vm_exists && die "VM $VM already exists (destroy it first, or set VM=...)"
	say "pulling and cloning $TART_IMAGE into $VM (the first pull is ~25 GB)"
	tart clone "$TART_IMAGE" "$VM"
	mkdir -p "$WORK"
}

cmd_up() {
	need tart
	vm_exists || die "no VM $VM (run: $0 create)"
	mkdir -p "$WORK"
	local args=()
	[ "${1:-}" = --headless ] && args+=(--no-graphics)
	if [ "$(vm_state)" = running ]; then
		say "$VM is already running"
	else
		say "booting $VM (log: $WORK/tart-run.log)"
		nohup tart run "${args[@]}" "$VM" >"$WORK/tart-run.log" 2>&1 &
		echo $! >"$WORK/tart-run.pid"
	fi
	local ip
	ip="$(vm_ip)"
	say "waiting for ssh on $ip"
	for _ in $(seq 1 60); do
		if vm_ssh true 2>/dev/null; then
			say "ssh is up: $VM_USER@$ip"
			vm_ssh 'sw_vers; uname -m'
			return 0
		fi
		sleep 5
	done
	die "ssh did not come up on $ip"
}

cmd_copy() {
	local src="${1:-}"
	[ -n "$src" ] || die "usage: $0 copy /path/to/Twinnel.app | Twinnel_*.zip | Twinnel_*.dmg"
	[ -e "$src" ] || die "not found: $src"
	mkdir -p "$WORK"
	local zip="$WORK/Twinnel.zip"
	case "$src" in
	*.app | *.app/)
		say "zipping $src"
		rm -f "$zip"
		ditto -c -k --keepParent "$src" "$zip"
		;;
	*.zip) cp "$src" "$zip" ;;
	*.dmg)
		say "copying the app out of $src"
		local mnt="$WORK/dmg"
		mkdir -p "$mnt"
		hdiutil attach -quiet -nobrowse -readonly -mountpoint "$mnt" "$src"
		rm -f "$zip"
		ditto -c -k --keepParent "$mnt/Twinnel.app" "$zip"
		hdiutil detach -quiet "$mnt"
		;;
	*) die "expected a .app, .zip or .dmg: $src" ;;
	esac
	say "copying into $VM:/Applications"
	vm_scp_to "$zip" /tmp/Twinnel.zip
	# A fresh copy each time; the old one is removed by its absolute path.
	vm_ssh 'set -e; rm -rf /Applications/Twinnel.app; ditto -x -k /tmp/Twinnel.zip /Applications; rm -f /tmp/Twinnel.zip
		ls -d /Applications/Twinnel.app
		/Applications/Twinnel.app/Contents/Resources/bin/twinnel version || true'
	cat <<-'EOF'
		Copied over ssh, so the app carries no quarantine flag: it opens without the
		"Open Anyway" step. To also test that step, download the release in the VM
		with Safari instead (see docs/testing-real-machines.md).
	EOF
}

cmd_fetch() {
	local tag="${1:-}"
	[ -n "$tag" ] || die "usage: $0 fetch TAG (e.g. v0.3.0)"
	need gh
	mkdir -p "$WORK/release"
	say "downloading the macOS app of $GH_REPO $tag"
	gh release download "$tag" -R "$GH_REPO" -D "$WORK/release" --clobber -p 'Twinnel_*_macos_universal.zip' -p SHA256SUMS
	(cd "$WORK/release" && shasum -a 256 -c SHA256SUMS --ignore-missing)
	local z
	z="$(find "$WORK/release" -maxdepth 1 -name 'Twinnel_*_macos_universal.zip' | head -n 1)"
	[ -n "$z" ] || die "no Twinnel_*_macos_universal.zip in release $tag"
	cmd_copy "$z"
}

cmd_manual() {
	cat <<'EOF'
Manual steps in the VM window (log in as admin / admin). Tick each one.

  [ ] 1. Open /Applications/Twinnel.app. The menu bar icon appears and the
         main window shows the first-run page.
  [ ] 2. Click 安装后台助手 / Install Helper; enter the admin password once.
         The page moves on to the subscription step.
  [ ] 3. Paste a subscription URL (a test subscription, never a personal one
         you would not put in a throwaway VM) and continue. Twinnel downloads
         the mihomo kernel, composes the config and starts mihomo.
  [ ] 4. Accept the system DNS step (DNS → 127.0.0.1).
  [ ] 5. Let it install Tailscale, then log in: the browser opens the
         Tailscale login page. Use a test tailnet or an ephemeral auth key.
  [ ] 6. The first-run page finishes on the status page; the menu bar icon
         shows the connected state (shape, not color) in light and dark.
  [ ] 7. Open the flyout from the menu bar icon: mode, node and Tailscale
         rows show and answer.
  [ ] 8. Optional: System Settings → General → Login Items lists Twinnel
         after 登录时打开 is turned on.

Then run:  scripts/e2e/macos-tart.sh check
EOF
}

cmd_check() {
	say "running the checklist in $VM"
	vm_scp_to "$here/macos-checklist.sh" /tmp/twinnel-macos-checklist.sh
	vm_ssh 'TWINNEL_E2E_OUT=/tmp/twinnel-e2e bash /tmp/twinnel-macos-checklist.sh'
}

cmd_collect() {
	mkdir -p "$WORK/out"
	vm_scp_from /tmp/twinnel-e2e "$WORK/out/"
	say "raw output in $WORK/out/twinnel-e2e (may contain the VM's tailnet and node names; keep it out of the repo)"
}

cmd_destroy() {
	need tart
	if vm_exists; then
		say "stopping and deleting $VM"
		tart stop "$VM" 2>/dev/null || true
		tart delete "$VM"
	else
		say "no VM $VM"
	fi
	if [ -d "$WORK" ]; then
		say "deleting $WORK"
		rm -rf "$WORK"
	fi
}

cmd_all() {
	local src="${1:-}"
	[ -n "$src" ] || die "usage: $0 all /path/to/Twinnel.app|.zip|.dmg"
	cmd_create
	cmd_up
	cmd_copy "$src"
	cmd_manual
	read -r -p "Press Enter when the manual steps are done… " _
	cmd_check
	echo "When done: $0 destroy"
}

cmd="${1:-}"
[ $# -gt 0 ] && shift
case "$cmd" in
create) cmd_create ;;
up) cmd_up "$@" ;;
copy) cmd_copy "$@" ;;
fetch) cmd_fetch "$@" ;;
manual) cmd_manual ;;
check) cmd_check ;;
collect) cmd_collect ;;
ssh) vm_ssh "$@" ;;
destroy) cmd_destroy ;;
all) cmd_all "$@" ;;
*)
	sed -n '2,/^set -euo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'
	exit 2
	;;
esac
