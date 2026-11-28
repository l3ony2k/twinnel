package status

import (
	"strings"
	"unicode/utf8"
)

// Flag emoji in node names: a pair of regional indicators (🇺🇸) or a black
// flag followed by tag characters and a cancel tag (🏴󠁧󠁢󠁳󠁣󠁴󠁿). Fonts
// without flag glyphs draw the pair as two letters ("us NODE-US"), which
// reads as part of the name; Windows has none (Segoe UI Emoji draws the
// letters), nor do Linux desktops without a color emoji font.

const (
	regionalA  = 0x1F1E6
	regionalZ  = 0x1F1FF
	blackFlag  = 0x1F3F4
	tagFirst   = 0xE0020
	tagLast    = 0xE007F
	variation  = 0xFE0F // emoji presentation, after a flag in some names
	zeroJoiner = 0x200D
)

func isRegional(r rune) bool { return r >= regionalA && r <= regionalZ }
func isTag(r rune) bool      { return r >= tagFirst && r <= tagLast }

// HasFlag reports whether s has a flag emoji.
func HasFlag(s string) bool {
	for _, r := range s {
		if isRegional(r) || r == blackFlag {
			return true
		}
	}
	return false
}

// StripFlags removes the flag emoji from a name for a display that can't
// draw them, and the space they leave: "🇺🇸 NODE-US" → "NODE-US",
// "HK 🇭🇰 01" → "HK 01". A name that is nothing but flags becomes its
// region codes in capitals ("🇯🇵" → "JP"), so it never shows empty. The
// name itself (selecting, copying, the controller) stays as it is.
func StripFlags(s string) string {
	if !HasFlag(s) {
		return s
	}
	var b, codes strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case isRegional(r):
			codes.WriteRune('A' + (r - regionalA))
			continue
		case r == blackFlag:
			for i+1 < len(rs) && isTag(rs[i+1]) {
				i++
			}
			continue
		case (r == variation || r == zeroJoiner) && i > 0 && (isRegional(rs[i-1]) || rs[i-1] == blackFlag || isTag(rs[i-1])):
			continue
		}
		b.WriteRune(r)
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if out == "" || !utf8.ValidString(out) {
		return codes.String()
	}
	// Separators the flag stood between ("🇺🇸-NODE", "🇺🇸 | NODE") lose
	// their left side: drop one left leading.
	for _, sep := range []string{"- ", "| ", "· ", "_ "} {
		if strings.HasPrefix(out, sep) {
			out = strings.TrimPrefix(out, sep)
			break
		}
	}
	if out == "" {
		return codes.String()
	}
	return out
}
