#!/usr/bin/env bash
# Build the Windows amd64 app zip from this checkout and deploy it to a
# Windows PC over SSH: unpack, install.ps1 (per user, no launch), upgrade
# the helper with the bundled CLI, then print `twinnel version` and
# `twinnel helper upgrade --check` so drift shows.
#
# Usage: scripts/e2e/windows-deploy.sh [--no-build] [--zip PATH] [--arch amd64|arm64] HOST
#
# HOST is an ssh destination (an alias from ~/.ssh/config) whose login shell
# is PowerShell 7 (`pwsh`) with an elevated token (an administrator over
# OpenSSH gets one without a UAC prompt). The helper upgrade needs it;
# without it Windows would show a UAC prompt on the PC's screen.
#
# Deploy only a merged main: the script refuses another branch or a dirty
# tree unless TWINNEL_DEPLOY_ANY_BRANCH=1.
#
# On the PC it uses %ProgramData%\TwinnelQA\deploy\<version> (kept, so a
# round can be redeployed) and installs to the default per-user folder
# (%LOCALAPPDATA%\Programs\Twinnel). It never touches the helper's state,
# mihomo's config or Tailscale; the helper upgrade restarts only the helper
# service.
set -euo pipefail
cd "$(dirname "$0")/../.."

build=1
zip=""
arch=amd64
while [ $# -gt 0 ]; do
	case "$1" in
	--no-build) build=0; shift ;;
	--zip) zip="$2"; build=0; shift 2 ;;
	--arch) arch="$2"; shift 2 ;;
	-h | --help) sed -n '2,20p' "$0"; exit 0 ;;
	-*) echo "unknown flag: $1" >&2; exit 2 ;;
	*) break ;;
	esac
done
host="${1:?usage: $0 [--no-build] [--zip PATH] HOST}"

if [ "${TWINNEL_DEPLOY_ANY_BRANCH:-0}" != 1 ]; then
	branch="$(git rev-parse --abbrev-ref HEAD)"
	main="$(git rev-parse main)"
	if [ "$(git rev-parse HEAD)" != "$main" ] && ! git merge-base --is-ancestor HEAD main; then
		echo "refusing: HEAD ($branch) is not on main; deploy only merged main (TWINNEL_DEPLOY_ANY_BRANCH=1 overrides)" >&2
		exit 1
	fi
	if ! git diff --quiet HEAD -- cmd core platform packaging; then
		echo "refusing: uncommitted product changes" >&2
		exit 1
	fi
fi

if [ "$build" = 1 ]; then
	scripts/build-release.sh --no-app --targets "" --windows-app "windows/$arch"
fi
if [ -z "$zip" ]; then
	zip="$(ls -t dist/Twinnel_*_windows_"$arch".zip 2>/dev/null | head -n 1)"
fi
[ -f "$zip" ] || { echo "no Windows zip (dist/Twinnel_*_windows_$arch.zip); build first" >&2; exit 1; }
name="$(basename "$zip" .zip)"
echo "==> deploying $name to $host"

# Remote PowerShell runs with -NoProfile semantics where we can; the
# login profile may print banner lines, which we drop.
rps() {
	ssh "$host" "$@" 2>&1 | { grep -v '^Shim:' || true; }
	return "${PIPESTATUS[0]}"
}

remote_dir='C:/ProgramData/TwinnelQA/deploy'
rps "New-Item -ItemType Directory -Force -Path '$remote_dir' | Out-Null"
scp -q "$zip" "$host:$remote_dir/$name.zip"

rps "\$ErrorActionPreference='Stop'
\$d='$remote_dir'
\$z=Join-Path \$d '$name.zip'
\$x=Join-Path \$d '$name'
if (Test-Path -LiteralPath \$x) { Remove-Item -LiteralPath \$x -Recurse -Force }
Expand-Archive -LiteralPath \$z -DestinationPath \$d -Force
Unblock-File -LiteralPath \$z
Get-ChildItem -LiteralPath \$x -Recurse -File | Unblock-File
# Processes such as an agent's twinnel mcp server keep bin\twinnel.exe open, and
# install.ps1 cannot overwrite it. Windows lets a running image be renamed:
# move the locked ones aside (they keep running the old build) instead of
# killing someone's agent session.
\$ib=Join-Path \$env:LOCALAPPDATA 'Programs\\Twinnel\\bin'
Get-ChildItem -LiteralPath \$ib -Filter '*.old-*' -EA SilentlyContinue | Remove-Item -Force -EA SilentlyContinue
foreach (\$f in Get-ChildItem -LiteralPath \$ib -Filter '*.exe' -EA SilentlyContinue) {
  try { [IO.File]::Open(\$f.FullName,'Open','ReadWrite','None').Dispose() }
  catch { \$n=\$f.FullName+'.old-'+(Get-Date -Format yyyyMMddHHmmss); Write-Output \"move aside (in use): \$(\$f.Name)\"; Move-Item -LiteralPath \$f.FullName -Destination \$n }
}
Write-Output '==> install.ps1 -NoLaunch'
& powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path \$x 'install.ps1') -NoLaunch
if (\$LASTEXITCODE -ne 0) { throw \"install.ps1 failed (\$LASTEXITCODE)\" }
\$cli=Join-Path \$env:LOCALAPPDATA 'Programs\\Twinnel\\bin\\twinnel.exe'
Write-Output '==> twinnel helper upgrade'
& \$cli helper upgrade --yes
if (\$LASTEXITCODE -ne 0) { throw \"helper upgrade failed (\$LASTEXITCODE)\" }
Write-Output '==> twinnel version'
& \$cli version
Write-Output '==> twinnel helper upgrade --check'
& \$cli helper upgrade --check
" || { echo "==> FAILED on $host" >&2; exit 1; }
echo "==> done: $name on $host"
