#!/usr/bin/env bash
# Read-only check of a Twinnel install on Linux (tested on Arch with Hyprland
# and with KDE Plasma). Run it as your normal user after a real install; it
# only READS state: systemctl, ip, resolvectl / resolv.conf, the session bus,
# and `twinnel status|doctor|helper status --json`. No sudo, no writes, no
# service changes.
#
# Prints one row per step (PASS / FAIL / WARN / SKIP) and exits 1 when any
# step fails. Raw JSON goes to $TWINNEL_E2E_OUT (default
# ${XDG_RUNTIME_DIR:-/tmp}/twinnel-e2e); it names your tailnet and nodes, so
# keep it out of the repo and out of bug reports unless redacted.
#
# Usage: scripts/e2e/linux-check.sh [--no-gui]
#   --no-gui   skip the desktop rows (a server or an ssh session)
#
# Environment: TWINNEL_BIN (default `twinnel` on PATH), TWINNEL_E2E_OUT,
# TWINNEL_APP_PROC (process name pattern of the GUI app; default
# '^(twinnel-app|Twinnel|twinnel-gui)$').
set -uo pipefail

gui=1
[ "${1:-}" = --no-gui ] && gui=0

out="${TWINNEL_E2E_OUT:-${XDG_RUNTIME_DIR:-/tmp}/twinnel-e2e}"
mkdir -p "$out"
bin="${TWINNEL_BIN:-$(command -v twinnel || true)}"
app_proc="${TWINNEL_APP_PROC:-^(twinnel-app|Twinnel|twinnel-gui)$}"

SOCKET=/var/run/twinnel/helper.sock
HELPER_UNIT=twinnel-helper
MIHOMO_UNIT_DEFAULT=twinnel-mihomo
TAILSCALED_UNIT=tailscaled

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

# jget FILE KEYPATH: one value from a JSON file, key path with dots and array
# indexes (Mihomo.Config.tun.device, checks.0.id). Booleans print as
# true/false; arrays print their length. Uses jq, else python3.
if command -v jq >/dev/null 2>&1; then
	jget() {
		local path
		path="$(printf '%s' "$2" | awk -F. '{ for (i = 1; i <= NF; i++) printf ($i ~ /^[0-9]+$/ ? "[%s]" : "[\"%s\"]"), $i }')"
		jq -r "try (.${path} | if type == \"array\" then length elif . == null then \"\" else . end) catch \"\"" "$1" 2>/dev/null || true
	}
elif command -v python3 >/dev/null 2>&1; then
	jget() {
		python3 -I - "$1" "$2" <<-'EOF' 2>/dev/null || true
			import json, sys
			v = json.load(open(sys.argv[1]))
			for k in sys.argv[2].split("."):
			    v = v[int(k)] if isinstance(v, list) else v.get(k) if isinstance(v, dict) else None
			    if v is None:
			        sys.exit(0)
			if isinstance(v, bool):
			    print("true" if v else "false")
			elif isinstance(v, list):
			    print(len(v))
			else:
			    print(v)
		EOF
	}
else
	echo "linux-check: needs jq or python3 to read the JSON" >&2
	exit 2
fi

unit_state() { systemctl is-active "$1" 2>/dev/null || true; }

# ---- 0. the CLI and the session -------------------------------------------------

if [ -n "$bin" ] && "$bin" version >"$out/version.txt" 2>&1; then
	row PASS "twinnel CLI runs" "$(head -n 1 "$out/version.txt")"
else
	row FAIL "twinnel CLI runs" "${bin:-twinnel not on PATH}"
	bin=""
fi
if id -nG | tr ' ' '\n' | grep -qxE 'sudo|wheel|twinnel'; then
	row PASS "socket group" "$(id -un) is in $(id -nG | tr ' ' '\n' | grep -xE 'sudo|wheel|twinnel' | head -n 1)"
else
	row WARN "socket group" "$(id -un) is in none of sudo, wheel, twinnel: the CLI cannot reach the helper without root"
fi

# ---- 1. helper ------------------------------------------------------------------

st="$(unit_state "$HELPER_UNIT")"
if [ "$st" = active ]; then
	row PASS "helper unit" "$HELPER_UNIT active"
else
	row FAIL "helper unit" "$HELPER_UNIT: ${st:-unknown}"
fi
if [ -S "$SOCKET" ]; then
	row PASS "helper socket" "$SOCKET ($(stat -c '%U:%G %a' "$SOCKET" 2>/dev/null))"
else
	row FAIL "helper socket" "$SOCKET missing"
fi
hs="$out/helper-status.json"
mihomo_unit="$MIHOMO_UNIT_DEFAULT"
if [ -n "$bin" ] && "$bin" helper status --json --timeout 15s >"$hs" 2>"$out/helper-status.err"; then
	row PASS "helper answers" "version $(jget "$hs" version)"
	l="$(jget "$hs" mihomo.label)"
	[ -n "$l" ] && mihomo_unit="$l"
else
	row FAIL "helper answers" "$(head -n 1 "$out/helper-status.err" 2>/dev/null)"
fi

# ---- 2. mihomo ------------------------------------------------------------------

st="$(unit_state "$mihomo_unit")"
if [ "$st" = active ]; then
	row PASS "mihomo unit" "$mihomo_unit active"
else
	row FAIL "mihomo unit" "$mihomo_unit: ${st:-unknown}"
fi
ss="$out/status.json"
if [ -z "$bin" ] || ! "$bin" status --json --timeout 15s >"$ss" 2>"$out/status.err"; then
	echo '{}' >"$ss"
fi
if [ "$(jget "$ss" Mihomo.Reachable)" = true ]; then
	row PASS "mihomo controller" "mihomo $(jget "$ss" Mihomo.Version), mode $(jget "$ss" Mihomo.Config.mode)"
else
	row FAIL "mihomo controller" "unreachable: $(jget "$ss" Mihomo.Err)"
fi

# ---- 3. TUN and routes ------------------------------------------------------------

dev="$(jget "$ss" Mihomo.Config.tun.device)"
if [ "$(jget "$ss" Mihomo.Config.tun.enable)" = true ]; then
	row PASS "TUN enabled" "device ${dev:-?}, stack $(jget "$ss" Mihomo.Config.tun.stack)"
else
	row FAIL "TUN enabled" "mihomo reports tun.enable=false"
fi
if [ -n "$dev" ] && ip -o link show dev "$dev" 2>/dev/null | grep -qE '<[^>]*UP'; then
	row PASS "TUN interface up" "$dev"
else
	row FAIL "TUN interface up" "${dev:-no device} not up"
fi
routedev() { ip -o route get "$1" 2>/dev/null | sed -n 's/.* dev \([^ ]*\).*/\1/p' | head -n 1; }
ifc="$(routedev 1.1.1.1)"
if [ -n "$dev" ] && [ "$ifc" = "$dev" ]; then
	row PASS "internet routed into TUN" "1.1.1.1 → $ifc"
else
	row FAIL "internet routed into TUN" "1.1.1.1 → ${ifc:-no route} (TUN ${dev:-?})"
fi

# ---- 4. system DNS ------------------------------------------------------------------

rc="$(awk '/^nameserver/{print $2; exit}' /etc/resolv.conf 2>/dev/null)"
upstream=""
if command -v resolvectl >/dev/null 2>&1; then
	upstream="$(resolvectl dns 2>/dev/null | awk '{ for (i = 2; i <= NF; i++) print $i }' | grep -x '127\.0\.0\.1' | head -n 1)"
fi
if [ "$rc" = 127.0.0.1 ]; then
	row PASS "system DNS" "/etc/resolv.conf → 127.0.0.1"
elif [ "$rc" = 127.0.0.53 ] && [ -n "$upstream" ]; then
	row PASS "system DNS" "systemd-resolved stub → 127.0.0.1"
else
	row WARN "system DNS" "resolv.conf → ${rc:-none}; 127.0.0.1 not set (mihomo's dns-hijack may still catch it; see health: system-dns)"
fi
if getent hosts example.com >/dev/null 2>&1; then
	row PASS "name resolution" "example.com resolves"
else
	row FAIL "name resolution" "example.com does not resolve"
fi

# ---- 5. Tailscale -------------------------------------------------------------------

st="$(unit_state "$TAILSCALED_UNIT")"
if [ "$st" = active ]; then
	row PASS "tailscaled unit" "$TAILSCALED_UNIT active"
else
	row FAIL "tailscaled unit" "$TAILSCALED_UNIT: ${st:-unknown}"
fi
if [ "$(jget "$ss" Tailscale.Reachable)" = true ]; then
	bs="$(jget "$ss" Tailscale.State.BackendState)"
	if [ "$bs" = Running ]; then
		row PASS "Tailscale logged in" "BackendState Running"
	else
		row FAIL "Tailscale logged in" "BackendState $bs"
	fi
else
	row FAIL "Tailscale LocalAPI" "unreachable: $(jget "$ss" Tailscale.Err)"
fi
tif="$(routedev 100.100.100.100)"
if [ "$tif" = tailscale0 ]; then
	row PASS "tailnet bypasses mihomo TUN" "100.100.100.100 → tailscale0"
else
	row FAIL "tailnet bypasses mihomo TUN" "100.100.100.100 → ${tif:-no route} (want tailscale0)"
fi
if ip rule show 2>/dev/null | grep -q 'lookup 52'; then
	row PASS "Tailscale policy rules" "ip rule has table 52"
else
	row WARN "Tailscale policy rules" "no 'lookup 52' rule (Tailscale's netfilter/routing mode may differ)"
fi

# ---- 6. overall status and health -------------------------------------------------

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
		sum="$(jget "$dj" "checks.$i.summary")"
		case "$(jget "$dj" "checks.$i.status")" in
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
if [ -n "$bin" ] && "$bin" setup status --json >"$out/setup-status.json" 2>/dev/null; then
	row PASS "setup status" "saved setup-status.json"
else
	row SKIP "setup status" "not in this build"
fi

# ---- 7. desktop (Hyprland, KDE) -----------------------------------------------------

if [ "$gui" = 1 ]; then
	de="${XDG_CURRENT_DESKTOP:-}"
	if [ -n "$de" ]; then
		row PASS "desktop session" "$de (${XDG_SESSION_TYPE:-?})"
	else
		row WARN "desktop session" "XDG_CURRENT_DESKTOP unset (run from a terminal inside the session)"
	fi
	if command -v busctl >/dev/null 2>&1 && busctl --user list 2>/dev/null | grep -q 'org.kde.StatusNotifierWatcher'; then
		row PASS "tray host" "org.kde.StatusNotifierWatcher on the session bus"
	else
		row WARN "tray host" "no StatusNotifierWatcher (Hyprland: add a tray module to waybar or similar)"
	fi
	if pgrep -- "$app_proc" >/dev/null 2>&1; then
		row PASS "app running" "process matches $app_proc"
	else
		row WARN "app running" "no process matches $app_proc"
	fi
fi

# ---- table ----------------------------------------------------------------------------

printf '\n%-5s  %-30s  %s\n' RESULT STEP DETAIL
printf '%-5s  %-30s  %s\n' ----- ------------------------------ ------
for r in "${rows[@]}"; do
	IFS=$'\t' read -r s step detail <<<"$r"
	printf '%-5s  %-30s  %s\n' "$s" "$step" "$detail"
done
printf '\n%d failed, %d warnings. Raw output: %s\n' "$fails" "$warns" "$out"
[ "$fails" -eq 0 ]
