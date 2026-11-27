#!/usr/bin/env bash
# Check an executable's format and architecture from its header bytes, so
# release checks do not depend on how a given version of file(1) words it.
#
#   scripts/check-binary.sh elf FILE amd64|arm64
#       a 64-bit little-endian ELF for that machine (an AppImage counts: its
#       runtime is an ELF with the image appended)
#   scripts/check-binary.sh pe FILE amd64|arm64 gui|console
#       a PE32+ for that machine with that subsystem
#
# Prints what it found; exits 1 with the reason when it does not match.
set -eu

die() { echo "check-binary: $*" >&2; exit 1; }

# u16 / u32 FILE OFFSET: little-endian unsigned integer at OFFSET.
bytes() { od -An -v -tu1 -j "$2" -N "$3" -- "$1" | tr -s ' \n' ' '; }
# shellcheck disable=SC2046 # split the bytes into $1..$n
u16() { set -- $(bytes "$1" "$2" 2); [ $# -eq 2 ] || return 1; echo $(($1 + $2 * 256)); }
# shellcheck disable=SC2046
u32() { set -- $(bytes "$1" "$2" 4); [ $# -eq 4 ] || return 1; echo $(($1 + $2 * 256 + $3 * 65536 + $4 * 16777216)); }
hex() { printf '0x%x' "$1"; }

[ $# -ge 3 ] || die "usage: $0 elf FILE ARCH | pe FILE ARCH gui|console"
kind=$1 f=$2 arch=$3
[ -f "$f" ] || die "$f: no such file"

case "$kind" in
elf)
	case "$arch" in
	amd64) want=62 ;;   # EM_X86_64
	arm64) want=183 ;;  # EM_AARCH64
	*) die "unknown arch $arch" ;;
	esac
	magic=$(bytes "$f" 0 4)
	[ "$magic" = " 127 69 76 70 " ] || die "$f: not an ELF file (first bytes:$magic)"
	class=$(bytes "$f" 4 2)
	[ "$class" = " 2 1 " ] || die "$f: not a 64-bit little-endian ELF (EI_CLASS, EI_DATA:$class)"
	m=$(u16 "$f" 18) || die "$f: truncated ELF header"
	[ "$m" = "$want" ] || die "$f: ELF machine $m, want $want ($arch)"
	echo "$f: ELF64 $arch"
	;;
pe)
	[ $# -eq 4 ] || die "usage: $0 pe FILE ARCH gui|console"
	case "$arch" in
	amd64) want=$((0x8664)) ;;
	arm64) want=$((0xaa64)) ;;
	*) die "unknown arch $arch" ;;
	esac
	case "$4" in
	gui) wantsub=2 ;;      # IMAGE_SUBSYSTEM_WINDOWS_GUI
	console) wantsub=3 ;;  # IMAGE_SUBSYSTEM_WINDOWS_CUI
	*) die "unknown subsystem $4" ;;
	esac
	[ "$(bytes "$f" 0 2)" = " 77 90 " ] || die "$f: no MZ header"
	pe=$(u32 "$f" 60) || die "$f: truncated DOS header"
	[ "$(bytes "$f" "$pe" 4)" = " 80 69 0 0 " ] || die "$f: no PE signature at $(hex "$pe")"
	m=$(u16 "$f" $((pe + 4))) || die "$f: truncated COFF header"
	[ "$m" = "$want" ] || die "$f: PE machine $(hex "$m"), want $(hex "$want") ($arch)"
	opt=$((pe + 24))
	om=$(u16 "$f" "$opt") || die "$f: truncated optional header"
	[ "$om" = $((0x20b)) ] || die "$f: optional header magic $(hex "$om"), want 0x20b (PE32+)"
	sub=$(u16 "$f" $((opt + 68))) || die "$f: truncated optional header"
	[ "$sub" = "$wantsub" ] || die "$f: subsystem $sub, want $wantsub ($4)"
	echo "$f: PE32+ $arch $4"
	;;
*) die "unknown kind $kind" ;;
esac
