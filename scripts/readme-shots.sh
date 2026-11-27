#!/bin/bash
# readme-shots.sh SCRATCH [OUT]: the README's screenshots, from synthetic
# data only, on a hermetic stack: fakemihomo (invented nodes, traffic,
# logs), a scratch helper on a temp socket seeded with a synthetic
# subscription and layers (cmd/app/testdata/readme), the Tailscale status
# fixture and a scratch HOME. Nothing reads or touches the live mihomo,
# Tailscale or the installed helper; the app never activates or shows a
# window (TWINNEL_SHOTS, TWINNEL_SHOTS_FLOW=readme).
#
# SCRATCH is a directory outside the repository (it is emptied); OUT
# defaults to docs/images. Needs: go, magick, pngquant, oxipng (brew).
#
# Output: OUT/{en,zh}/<shot>-{light,dark}.png at 2x, each cropped to the
# window content with rounded corners (headless captures have no title
# bar), and OUT/tray-states.png.
set -euo pipefail

[ $# -ge 1 ] || { echo "usage: $0 SCRATCH [OUT]" >&2; exit 2; }
REPO=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$1"
S=$(cd "$1" && pwd)
OUT=${2:-$REPO/docs/images}
case "$S/" in "$REPO"/*) echo "SCRATCH must be outside the repository" >&2; exit 2 ;; esac
case "$S" in "$HOME" | / | "") echo "refusing SCRATCH=$S" >&2; exit 2 ;; esac
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
for t in go magick pngquant oxipng; do command -v $t >/dev/null || { echo "missing $t" >&2; exit 2; }; done

FIX=$REPO/cmd/app/testdata/readme
TS=$FIX/tailscale-status.json # no exit node in use: every check passes
CTRL=127.0.0.1:19894
SOCKDIR=$(mktemp -d /tmp/twinnel-readme.XXXXXX) # a unix socket path must stay under 104 bytes
SOCK=$SOCKDIR/twinnel.sock
export TWINNEL_SOCKET=$SOCK # the CLI below talks to the scratch helper only

rm -rf "$S/run" "$S/raw"
mkdir -p "$S/bin" "$S/run" "$S/raw"
cd "$REPO"
echo "building"
go build -o "$S/bin/fakemihomo" ./cmd/app/internal/fakemihomo
go build -o "$S/bin/twinnel-helper" ./cmd/helper
go build -o "$S/bin/twinnel" ./cmd/cli
go build -o "$S/bin/twinnel-app" ./cmd/app
# devfake writes the fake `mihomo -t` the helper validates with.
go build -o "$S/bin/devfake" ./cmd/helper/internal/devfake

PIDS=()
cleanup() {
	for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
	wait 2>/dev/null || true
	rm -rf "$SOCKDIR"
}
trap cleanup EXIT

front() { osascript -e 'tell application "System Events" to get name of first process whose frontmost is true' 2>/dev/null || echo "?"; }
FRONT_BEFORE=$(front)

helper_up() { # $1 run dir
	"$S/bin/twinnel-helper" --state-dir "$1/state" --socket "$SOCK" --foreground --secrets file --group staff \
		--tailnet-status-file "$TS" --no-health --no-controller-owner --tailnet-iface none \
		--takeover-services-dir "$1/empty" --takeover-home "$1/home" >>"$1/helper.log" 2>&1 &
	HELPER=$!
	PIDS+=("$HELPER")
	for _ in $(seq 1 100); do [ -S "$SOCK" ] && return 0; sleep 0.1; done
	echo "helper did not start, see $1/helper.log" >&2
	exit 1
}

for lang in en zh; do
	R=$S/run/$lang
	mkdir -p "$R/state" "$R/empty" "$R/home" "$R/conf" "$R/fakebin"
	if [ $lang = en ]; then
		SUB="Example Cloud"
		LAN="LAN direct" DEV="Dev tools"
		R1="Subscription and base settings" R2="Home network direct" R3="Developer services through the proxy"
		APPLANG=en
	else
		SUB="示例机场"
		LAN="局域网直连" DEV="开发工具"
		R1="订阅与基础设置" R2="家庭网络直连" R3="开发服务走代理"
		APPLANG=zh-Hans
	fi

	"$S/bin/fakemihomo" --listen $CTRL --lang $lang >"$R/fakemihomo.log" 2>&1 &
	FAKE=$!
	PIDS+=("$FAKE")
	"$S/bin/devfake" --listen 127.0.0.1:19895 --write-mihomo "$R/fakebin" >"$R/devfake.log" 2>&1 &
	DF=$!
	for _ in $(seq 1 50); do [ -x "$R/fakebin/mihomo" ] && break; sleep 0.1; done
	kill $DF 2>/dev/null || true; wait $DF 2>/dev/null || true

	# First start writes the default settings; point them at the fakes.
	helper_up "$R"
	kill $HELPER; wait $HELPER 2>/dev/null || true
	python3 - "$R" "$CTRL" <<'PY'
import json, sys
r, ctrl = sys.argv[1], sys.argv[2]
p = r + "/state/settings.json"
d = json.load(open(p))
m = d.setdefault("mihomo", {})
m.update(binary=r + "/fakebin/mihomo", config_dir=r + "/conf", config_file=r + "/conf/config.yaml", controller=ctrl)
d["auto_apply"] = False # the last layer change stays pending, for the preview
d["leak_guard_groups"] = ["OpenAI"]
json.dump(d, open(p, "w"), indent=2)
PY
	helper_up "$R"

	D="$S/bin/twinnel"
	$D sub put main --name "$SUB" --file "$FIX/subscription-$lang.yaml" --interval 1440 >/dev/null
	$D layer put base "$FIX/base.yaml" --stage base --name "Base" >/dev/null
	$D compose --apply --reason "$R1" >/dev/null
	sleep 1
	$D layer put lan-direct "$FIX/lan-direct.yaml" --name "$LAN" >/dev/null
	$D compose --apply --reason "$R2" >/dev/null
	sleep 1
	$D layer put dev-tools "$FIX/dev-tools.yaml" --name "$DEV" >/dev/null
	$D compose --apply --reason "$R3" >/dev/null
	$D layer put dev-tools "$FIX/dev-tools-2.yaml" --name "$DEV" >/dev/null # pending

	for theme in light dark; do
		for part in main health; do
			dir=$S/raw/$lang-$theme-$part
			mkdir -p "$dir"
			extra=()
			[ $part = health ] && extra=(TWINNEL_README_PART=health TWINNEL_HEALTH_FIXTURE="$FIX/health-warn.json")
			echo "shots: $lang $theme $part"
			env -i PATH="$PATH" HOME="$R/home" TMPDIR="$S/run" \
				TWINNEL_SHOTS="$dir" TWINNEL_SHOTS_FLOW=readme TWINNEL_SOCKET="$SOCK" TWINNEL_CONTROLLER=$CTRL \
				TWINNEL_TAILSCALE_STATUS_FILE="$TS" TWINNEL_LANG=$APPLANG TWINNEL_THEME=$theme TWINNEL_READ_ONLY=1 \
				${extra[@]+"${extra[@]}"} "$S/bin/twinnel-app" >"$dir/app.log" 2>&1
		done
	done
	kill $HELPER $FAKE 2>/dev/null || true
	wait $HELPER $FAKE 2>/dev/null || true
done

FRONT_AFTER=$(front)
echo "frontmost app before: $FRONT_BEFORE, after: $FRONT_AFTER"

# Post-process: round the window's corners (a macOS window's radius at 2x)
# and add a hairline border, then quantize.
frame() { # in out
	local w h
	w=$(magick identify -format %w "$1")
	h=$(magick identify -format %h "$1")
	magick "$1" \( -size "${w}x${h}" xc:none -fill white -draw "roundrectangle 0,0,$((w - 1)),$((h - 1)),20,20" \) \
		-alpha set -compose DstIn -composite \
		\( -size "${w}x${h}" xc:none -fill none -stroke 'rgba(0,0,0,0.18)' -strokewidth 2 -draw "roundrectangle 1,1,$((w - 2)),$((h - 2)),19,19" \) \
		-compose Over -composite "$2"
}
rm -rf "$OUT/en" "$OUT/zh"
mkdir -p "$OUT/en" "$OUT/zh"
for lang in en zh; do
	for theme in light dark; do
		for f in "$S/raw/$lang-$theme-main"/*.png; do
			n=$(basename "$f" .png)
			case $n in tray-*) continue ;; esac
			frame "$f" "$OUT/$lang/$n-$theme.png"
		done
		frame "$S/raw/$lang-$theme-health/health.png" "$OUT/$lang/health-warn-$theme.png"
	done
done

# The menu bar icon states: each template icon on a light and a dark bar.
T=$S/raw/en-light-main
STATES="twinnel twinnel-warn twinnel-error twinnel-unknown twinnel-tsdown twinnel-mode twinnel-nohelper"
row() { # bg fg out
	local args=()
	for s in $STATES; do
		args+=(\( "$T/tray-$s.png" -fill "$2" -colorize 100 -background "$1" -gravity center -extent 72x56 \))
	done
	magick -background "$1" "${args[@]}" +append "$3"
}
row '#ececec' '#1d1d1f' "$S/raw/tray-light.png"
row '#1e1e1e' '#f5f5f7' "$S/raw/tray-dark.png"
magick "$S/raw/tray-light.png" "$S/raw/tray-dark.png" -append "$OUT/tray-states.png"

pngquant --force --skip-if-larger --quality 70-92 --ext .png "$OUT"/*.png "$OUT"/en/*.png "$OUT"/zh/*.png || true
oxipng -q -o 3 --strip safe "$OUT"/*.png "$OUT"/en/*.png "$OUT"/zh/*.png
du -sh "$OUT"
