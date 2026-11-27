#!/usr/bin/env bash
# Fail if tracked files (or files about to be committed) contain any of the
# private patterns listed one per line in a file kept OUTSIDE the repo.
#
# Usage: scripts/check-no-personal-data.sh [PATTERN_FILE]
#   default PATTERN_FILE: $TWINNEL_PRIVATE_PATTERNS, else ../../scratch/private-patterns.txt
#
# Patterns are fixed strings, matched case-insensitively; blank lines and
# lines starting with # are ignored. Short patterns match whole words only
# (fewer than 5 characters), so a 3-letter name does not flag every word that
# contains it. The patterns themselves never appear in this script or the
# repo. Exit status: 0 clean, 1 matches found, 2 usage error.
set -euo pipefail
cd "$(dirname "$0")/.."

pf="${1:-${TWINNEL_PRIVATE_PATTERNS:-../../scratch/private-patterns.txt}}"
if [ ! -f "$pf" ]; then
	echo "pattern file not found: $pf (skipping)" >&2
	exit 2
fi

long=$(mktemp)
short=$(mktemp)
trap 'rm -f "$long" "$short"' EXIT
grep -v '^[[:space:]]*#' "$pf" | sed 's/[[:space:]]*$//' | grep -v '^$' |
	awk -v l="$long" -v s="$short" 'length($0) >= 5 { print > l; next } { print > s }' || true

# Tracked files plus untracked, not ignored ones: what a commit could include.
files=$(git ls-files -co --exclude-standard)
found=0
if [ -s "$long" ]; then
	if printf '%s\n' "$files" | xargs grep -n -I -i -F -f "$long" -- 2>/dev/null; then found=1; fi
fi
if [ -s "$short" ]; then
	if printf '%s\n' "$files" | xargs grep -n -I -i -w -F -f "$short" -- 2>/dev/null; then found=1; fi
fi
# File names too.
if printf '%s\n' "$files" | grep -i -F -f <(cat "$long" "$short") ; then found=1; fi

n=$(cat "$long" "$short" | wc -l | tr -d ' ')
if [ "$found" = 1 ]; then
	echo "personal data check: FAILED (matches above; $n patterns)" >&2
	exit 1
fi
echo "personal data check: clean ($n patterns, $(printf '%s\n' "$files" | wc -l | tr -d ' ') files)"
