#!/bin/bash
# shot-flows.sh SCRATCH [FLOW...]: run shot flows (TWINNEL_SHOTS_FLOW) on a
# hermetic stack and fail when one fails, as when it saves the same
# picture twice (the dedup guard, cmd/app/shots.go). Default flows:
# default, lifecycle, components.
#
# The stack is CONTRIBUTING.md's recipe: fakemihomo (invented nodes,
# traffic, logs), devfake's fake `mihomo -t`, a scratch helper on a temp
# socket (a named pipe on Windows, unelevated) pointed at the fake
# controller, the Tailscale status fixture and a scratch HOME. Nothing
# reads or touches a live mihomo, Tailscale or installed helper, and the
# app never activates or shows a window. Runs on macOS, Linux and Windows
# (Git Bash); used by CI's windows job.
#
# SCRATCH is a directory outside the repository (its run/ and out/ are
# emptied). Needs go and python3.
set -euo pipefail

[ $# -ge 1 ] || { echo "usage: $0 SCRATCH [FLOW...]" >&2; exit 2; }
REPO=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$1"
S=$(cd "$1" && pwd)
shift
FLOWS=("$@")
[ ${#FLOWS[@]} -gt 0 ] || FLOWS=(default lifecycle components)
case "$S/" in "$REPO"/*) echo "SCRATCH must be outside the repository" >&2; exit 2 ;; esac
case "$S" in "$HOME" | / | "") echo "refusing SCRATCH=$S" >&2; exit 2 ;; esac

EXE= WIN=
case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) EXE=.exe WIN=1 ;; esac
native() { if [ -n "$WIN" ]; then cygpath -w "$1"; else printf '%s' "$1"; fi; }

TS=$REPO/cmd/app/testdata/shots-tailscale-status.json
CTRL=127.0.0.1:19894
if [ -n "$WIN" ]; then
	SOCK='\\.\pipe\twinnel-shots-'$$
	SOCKDIR=
else
	SOCKDIR=$(mktemp -d /tmp/twinnel-shots.XXXXXX) # a unix socket path must stay under 104 bytes
	SOCK=$SOCKDIR/twinnel.sock
fi

rm -rf "$S/run" "$S/out"
mkdir -p "$S/bin" "$S/run/state" "$S/run/empty" "$S/run/home" "$S/run/conf" "$S/run/fakebin" "$S/out"
cd "$REPO"
echo "building"
go build -o "$S/bin/fakemihomo$EXE" ./cmd/app/internal/fakemihomo
go build -o "$S/bin/devfake$EXE" ./cmd/helper/internal/devfake
go build -o "$S/bin/twinnel-helper$EXE" ./cmd/helper
go build -o "$S/bin/twinnel-app$EXE" ./cmd/app
go build -o "$S/bin/twinnel$EXE" ./cmd/cli

PIDS=()
cleanup() {
	for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
	wait 2>/dev/null || true
	[ -z "$SOCKDIR" ] || rm -rf "$SOCKDIR"
}
trap cleanup EXIT

R=$S/run
"$S/bin/fakemihomo$EXE" --listen $CTRL >"$R/fakemihomo.log" 2>&1 &
PIDS+=($!)
"$S/bin/devfake$EXE" --listen 127.0.0.1:19895 --write-mihomo "$(native "$R/fakebin")" >"$R/devfake.log" 2>&1 &
DF=$!
for _ in $(seq 1 50); do [ -e "$R/fakebin/mihomo" ] || [ -e "$R/fakebin/mihomo.cmd" ] && break; sleep 0.1; done
kill $DF 2>/dev/null || true
wait $DF 2>/dev/null || true
MIHOMO=$R/fakebin/mihomo
[ -e "$MIHOMO.cmd" ] && MIHOMO=$MIHOMO.cmd

helper_up() {
	local group=()
	[ -n "$WIN" ] || group=(--group "$(id -gn)")
	"$S/bin/twinnel-helper$EXE" --state-dir "$(native "$R/state")" --socket "$SOCK" --foreground --secrets file "${group[@]}" \
		--tailnet-status-file "$(native "$TS")" --no-health --no-controller-owner --tailnet-iface none \
		--takeover-services-dir "$(native "$R/empty")" --takeover-home "$(native "$R/home")" >>"$R/helper.log" 2>&1 &
	HELPER=$!
	PIDS+=("$HELPER")
	for _ in $(seq 1 100); do [ -f "$R/state/settings.json" ] && return 0; sleep 0.1; done
	echo "helper did not start, see $R/helper.log" >&2
	exit 1
}

# The first start writes the default settings; point them at the fakes
# (never the live controller on 9090), then start again.
helper_up
sleep 1
kill $HELPER
wait $HELPER 2>/dev/null || true
python3 - "$(native "$R")" "$(native "$MIHOMO")" "$CTRL" <<'PY'
import json, os, sys
r, mihomo, ctrl = sys.argv[1:4]
p = os.path.join(r, "state", "settings.json")
d = json.load(open(p))
m = d.setdefault("mihomo", {})
conf = os.path.join(r, "conf")
m.update(binary=mihomo, config_dir=conf, config_file=os.path.join(conf, "config.yaml"), controller=ctrl)
json.dump(d, open(p, "w"), indent=2)
PY
helper_up
sleep 2

# Synthetic content for the config pages (the README's fixtures): a
# subscription, a base layer and a rule layer, applied, then one change
# left pending for the preview.
FIX=$REPO/cmd/app/testdata/readme
D() { TWINNEL_SOCKET="$SOCK" HOME="$R/home" "$S/bin/twinnel$EXE" "$@" >>"$R/cli.log" 2>&1; }
D sub put main --name "示例机场" --file "$(native "$FIX/subscription-zh.yaml")" --interval 1440
D layer put base "$(native "$FIX/base.yaml")" --stage base --name "Base"
D compose --apply --reason "订阅与基础设置"
D layer put lan-direct "$(native "$FIX/lan-direct.yaml")" --name "局域网直连"
D compose --apply --reason "家庭网络直连"
D layer put dev-tools "$(native "$FIX/dev-tools.yaml")" --name "开发工具"

fail=0
for flow in "${FLOWS[@]}"; do
	dir=$S/out/$flow
	mkdir -p "$dir"
	echo "flow $flow"
	set +e
	env -i PATH="$PATH" HOME="$R/home" USERPROFILE="$(native "$R/home")" APPDATA="$(native "$R/home/AppData/Roaming")" \
		LOCALAPPDATA="$(native "$R/home/AppData/Local")" TEMP="$(native "$R")" TMP="$(native "$R")" TMPDIR="$R" \
		SYSTEMROOT="${SYSTEMROOT:-}" WINDIR="${WINDIR:-}" \
		TWINNEL_SHOTS="$(native "$dir")" TWINNEL_SHOTS_FLOW="$flow" TWINNEL_SOCKET="$SOCK" TWINNEL_CONTROLLER=$CTRL \
		TWINNEL_TAILSCALE_STATUS_FILE="$(native "$TS")" TWINNEL_LANG=zh-Hans TWINNEL_READ_ONLY=1 \
		"$S/bin/twinnel-app$EXE" >"$dir/app.log" 2>&1
	code=$?
	set -e
	n=$(find "$dir" -name '*.png' | wc -l | tr -d ' ')
	if grep -q "SAME PICTURE AS" "$dir/app.log" || [ $code -ne 0 ] || [ "$n" = 0 ]; then
		echo "FAIL $flow: exit $code, $n pictures" >&2
		grep -E "SAME PICTURE|shots:|panic|fatal" "$dir/app.log" >&2 || tail -20 "$dir/app.log" >&2
		fail=1
	else
		echo "ok $flow: $n pictures"
	fi
done
exit $fail
