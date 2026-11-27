#!/usr/bin/env bash
# Fresh-setup checklist for macOS. Runs INSIDE the test VM (scripts/e2e/
# macos-tart.sh check copies it there) after the manual GUI steps, and only
# READS state: the helper's socket, launchd, the routing table, the resolver
# configuration, and `twinnel status|doctor|helper status --json`. It never
# writes, never uses sudo and never loads or unloads anything.
#
# Prints one row per step (PASS / FAIL / WARN / SKIP) and exits 1 when any
# step fails. Raw JSON goes to $TWINNEL_E2E_OUT (default /tmp/twinnel-e2e).
#
# It refuses to run outside a VM unless TWINNEL_E2E_ALLOW_HOST=1 (still
# read-only, but a real Mac's output names its real tailnet and nodes).
#
# Environment: TWINNEL_BIN (the CLI; default the app's bundled copy, else
# `twinnel` on PATH), TWINNEL_E2E_OUT.
set -uo pipefail

if [ "$(sysctl -n kern.hv_vmm_present 2>/dev/null)" != 1 ] && [ "${TWINNEL_E2E_ALLOW_HOST:-}" != 1 ]; then
	echo "macos-checklist: not in a VM; set TWINNEL_E2E_ALLOW_HOST=1 to run it here anyway (read-only)" >&2
	exit 2
fi

out="${TWINNEL_E2E_OUT:-/tmp/twinnel-e2e}"
mkdir -p "$out"

bin="${TWINNEL_BIN:-}"
if [ -z "$bin" ]; then
	if [ -x /Applications/Twinnel.app/Contents/Resources/bin/twinnel ]; then
		bin=/Applications/Twinnel.app/Contents/Resources/bin/twinnel
	else
		bin="$(command -v twinnel || true)"
	fi
fi

SOCKET=/var/run/twinnel/helper.sock
HELPER_LABEL=com.l3ony2k.twinnel.helper
MIHOMO_LABEL_DEFAULT=com.l3ony2k.twinnel.mihomo

rows=()
fails=0
warns=0
row() { # status step detail
	rows+=("$1"$'\t'"$2"$'\t'"$3")
	case "$1" in
	FAIL) fails=$((fails + 1)) ;;
	WARN) warns=$((warns + 1)) ;;
	esac
}

# jget FILE KEYPATH: one value from a JSON file (plutil reads JSON; key paths
# use dots and array indexes). Empty when missing.
jget() { plutil -extract "$2" raw -o - "$1" 2>/dev/null || true; }

# launchd_state LABEL: the "state = …" of a system service, empty if unknown.
launchd_state() { launchctl print "system/$1" 2>/dev/null | awk -F' = ' '/^\tstate = /{print $2; exit}'; }

# ---- 0. the app and the CLI ------------------------------------------------

if [ -d /Applications/Twinnel.app ]; then
	row PASS "app installed" "/Applications/Twinnel.app"
else
	row FAIL "app installed" "/Applications/Twinnel.app missing"
fi
if [ -n "$bin" ] && "$bin" version >"$out/version.txt" 2>&1; then
	row PASS "twinnel CLI runs" "$(head -n 1 "$out/version.txt")"
else
	row FAIL "twinnel CLI runs" "${bin:-not found}"
	bin=""
fi
if pgrep -xq Twinnel; then
	row PASS "app running" "Twinnel process present"
else
	row WARN "app running" "no Twinnel process (open it to check the menu bar)"
fi

# ---- 1. helper ---------------------------------------------------------------

if [ -S "$SOCKET" ]; then
	row PASS "helper socket" "$SOCKET"
else
	row FAIL "helper socket" "$SOCKET missing"
fi
st="$(launchd_state "$HELPER_LABEL")"
if [ "$st" = running ]; then
	row PASS "helper LaunchDaemon" "$HELPER_LABEL running"
else
	row FAIL "helper LaunchDaemon" "$HELPER_LABEL: ${st:-not loaded}"
fi
hs="$out/helper-status.json"
mihomo_label="$MIHOMO_LABEL_DEFAULT"
if [ -n "$bin" ] && "$bin" helper status --json --timeout 15s >"$hs" 2>"$out/helper-status.err"; then
	row PASS "helper answers" "version $(jget "$hs" version)"
	l="$(jget "$hs" mihomo.label)"
	[ -n "$l" ] && mihomo_label="$l"
else
	row FAIL "helper answers" "$(head -n 1 "$out/helper-status.err" 2>/dev/null)"
fi

# ---- 2. mihomo ----------------------------------------------------------------

if [ -f "/Library/LaunchDaemons/$mihomo_label.plist" ]; then
	row PASS "mihomo LaunchDaemon plist" "/Library/LaunchDaemons/$mihomo_label.plist"
else
	row FAIL "mihomo LaunchDaemon plist" "/Library/LaunchDaemons/$mihomo_label.plist missing"
fi
st="$(launchd_state "$mihomo_label")"
if [ "$st" = running ]; then
	row PASS "mihomo LaunchDaemon" "$mihomo_label running"
else
	row FAIL "mihomo LaunchDaemon" "$mihomo_label: ${st:-not loaded}"
fi

ss="$out/status.json"
if [ -n "$bin" ] && "$bin" status --json --timeout 15s >"$ss" 2>"$out/status.err"; then
	:
else
	echo '{}' >"$ss"
fi
if [ "$(jget "$ss" Mihomo.Reachable)" = true ]; then
	row PASS "mihomo controller" "mihomo $(jget "$ss" Mihomo.Version), mode $(jget "$ss" Mihomo.Config.mode)"
else
	row FAIL "mihomo controller" "unreachable: $(jget "$ss" Mihomo.Err)"
fi

# ---- 3. TUN -------------------------------------------------------------------

dev="$(jget "$ss" Mihomo.Config.tun.device)"
if [ "$(jget "$ss" Mihomo.Config.tun.enable)" = true ]; then
	row PASS "TUN enabled" "device ${dev:-?}, stack $(jget "$ss" Mihomo.Config.tun.stack)"
else
	row FAIL "TUN enabled" "mihomo reports tun.enable=false"
fi
if [ -n "$dev" ] && ifconfig "$dev" 2>/dev/null | grep -q 'flags=.*UP'; then
	row PASS "TUN interface up" "$dev"
else
	row FAIL "TUN interface up" "${dev:-no device} not up"
fi
ifc="$(route -n get 1.1.1.1 2>/dev/null | awk '/interface:/{print $2}')"
if [ -n "$dev" ] && [ "$ifc" = "$dev" ]; then
	row PASS "internet routed into TUN" "1.1.1.1 → $ifc"
else
	row FAIL "internet routed into TUN" "1.1.1.1 → ${ifc:-no route} (TUN ${dev:-?})"
fi

# ---- 4. system DNS ------------------------------------------------------------

ns="$(scutil --dns 2>/dev/null | awk '/^resolver #1/{f=1} f && /nameserver\[0\]/{print $3; exit}')"
if [ "$ns" = 127.0.0.1 ]; then
	row PASS "system DNS" "resolver #1 → 127.0.0.1"
else
	row FAIL "system DNS" "resolver #1 → ${ns:-none} (want 127.0.0.1)"
fi
if dscacheutil -q host -a name example.com 2>/dev/null | grep -q '^ip_address'; then
	row PASS "name resolution" "example.com resolves"
else
	row FAIL "name resolution" "example.com does not resolve"
fi

# ---- 5. Tailscale ---------------------------------------------------------------

if pgrep -xq tailscaled; then
	tl=""
	for l in com.tailscale.tailscaled homebrew.mxcl.tailscale; do
		[ "$(launchd_state "$l")" = running ] && tl="$l" && break
	done
	if [ -n "$tl" ]; then
		row PASS "tailscaled running" "LaunchDaemon $tl"
	else
		row WARN "tailscaled running" "process runs, but not from a known LaunchDaemon"
	fi
elif pgrep -q -f 'io.tailscale.ipn.macsys|IPNExtension'; then
	row WARN "tailscaled running" "the Tailscale app's system extension runs instead of tailscaled"
else
	row FAIL "tailscaled running" "no tailscaled process"
fi
if [ "$(jget "$ss" Tailscale.Reachable)" = true ]; then
	bs="$(jget "$ss" Tailscale.State.BackendState)"
	if [ "$bs" = Running ]; then
		row PASS "Tailscale logged in" "BackendState Running, install $(jget "$ss" Tailscale.Install.Kind)"
	else
		row FAIL "Tailscale logged in" "BackendState $bs (manual step 5)"
	fi
else
	row FAIL "Tailscale LocalAPI" "unreachable: $(jget "$ss" Tailscale.Err)"
fi
tif="$(route -n get 100.100.100.100 2>/dev/null | awk '/interface:/{print $2}')"
if [ -n "$tif" ] && [ "$tif" != "$dev" ]; then
	row PASS "tailnet bypasses mihomo TUN" "100.100.100.100 → $tif"
else
	row FAIL "tailnet bypasses mihomo TUN" "100.100.100.100 → ${tif:-no route} (mihomo TUN ${dev:-?})"
fi

# ---- 6. overall status and health ---------------------------------------------

lvl="$(jget "$ss" Level)"
case "$lvl" in
ok) row PASS "status level" "ok" ;;
warn) row WARN "status level" "warn" ;;
*) row FAIL "status level" "${lvl:-no snapshot}" ;;
esac

dj="$out/doctor.json"
if [ -n "$bin" ] && "$bin" doctor --json >"$dj" 2>"$out/doctor.err"; then
	n="$(jget "$dj" checks)"
	i=0
	while [ "$i" -lt "${n:-0}" ]; do
		id="$(jget "$dj" "checks.$i.id")"
		s="$(jget "$dj" "checks.$i.status")"
		sum="$(jget "$dj" "checks.$i.summary")"
		case "$s" in
		ok) row PASS "health: $id" "$sum" ;;
		warn) row WARN "health: $id" "$sum" ;;
		skip) row SKIP "health: $id" "$sum" ;;
		*) row FAIL "health: $id" "$sum" ;;
		esac
		i=$((i + 1))
	done
	ov="$(jget "$dj" overall)"
	case "$ov" in
	ok) row PASS "health overall" "ok ($n checks)" ;;
	warn) row WARN "health overall" "warn" ;;
	*) row FAIL "health overall" "${ov:-?}" ;;
	esac
else
	row FAIL "health (twinnel doctor)" "$(head -n 1 "$out/doctor.err" 2>/dev/null)"
fi

# `twinnel setup status` exists from the fresh-setup release on; record it
# when the build has it.
if [ -n "$bin" ] && "$bin" setup status --json >"$out/setup-status.json" 2>/dev/null; then
	row PASS "setup status" "saved setup-status.json"
else
	row SKIP "setup status" "not in this build"
fi

# ---- table ----------------------------------------------------------------------

printf '\n%-5s  %-30s  %s\n' RESULT STEP DETAIL
printf '%-5s  %-30s  %s\n' ----- ------------------------------ ------
for r in "${rows[@]}"; do
	IFS=$'\t' read -r s step detail <<<"$r"
	printf '%-5s  %-30s  %s\n' "$s" "$step" "$detail"
done
printf '\n%d failed, %d warnings. Raw output: %s\n' "$fails" "$warns" "$out"
[ "$fails" -eq 0 ]
