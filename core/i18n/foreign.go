package i18n

import (
	"regexp"
	"strings"
	"unicode"
)

// English must not leak into the Chinese UI (design review U57): every
// zh text, and every coded helper message worded in zh, may hold ASCII
// words of 4 or more letters only when they are names duet keeps as
// written (products, config syntax, commands).
var allowedASCII = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`
		mihomo Tailscale tailscale tailscaled tailnet duet MagicDNS DERP YAML yaml
		Clash Party Verge Homebrew brew LaunchDaemon LaunchDaemons GitHub Alpha
		fake http https sudo security find generic password plist json
		DIRECT REJECT GLOBAL PROXY Shields TCP UDP HTTP HTTPS SOCKS IPv4 IPv6 IPLC
		prepend append rules proxies groups proxy nameserver policy filter mode
		range route exclude address listen enable secret external controller
		store selected profile tproxy redir socks mixed port allow bind ifconfig
		Finder Safari macOS Linux Windows Library Application Support Logs
		Keychain Login Items Option Command Shift Control Return Enter Space
		Delete Escape arch darwin amd64 arm64 stable alpha latest version
		with doctor egress verbose root admin systemd hosts whitelist Standalone
		upgrade DOMAIN SUFFIX KEYWORD example LaunchAgent LaunchAgents laptop
		GEOIP GEOSITE Agent Claude Code Codex Cursor`) {
		allowedASCII[strings.ToLower(w)] = true
	}
}

var (
	asciiWord = regexp.MustCompile(`[A-Za-z]{4,}`)
	syntaxRe  = regexp.MustCompile("`[^`]*`|%\\[?\\d*\\]?[a-z]|[\\w.-]+\\.(yaml|yml|json|plist|app|sh)\\b|[a-zA-Z][a-zA-Z0-9]*([-_][a-zA-Z0-9]+)+|[\\w-]+(\\.[\\w-]+)+/?\\S*|https?://\\S+")
)

// EnglishWords returns the English words in a Chinese UI text that are
// not names duet keeps as written (design review U57). Tests use it on
// whatever a page shows.
func EnglishWords(s string) []string {
	// Placeholders and config keys in quotes or code spans are syntax.
	s = syntaxRe.ReplaceAllString(s, " ")
	var out []string
	for _, w := range asciiWord.FindAllString(s, -1) {
		if !allowedASCII[strings.ToLower(w)] {
			out = append(out, w)
		}
	}
	return out
}

// HasHan reports whether s holds Chinese characters (or full-width
// punctuation), which an English UI text must not.
func HasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || (r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef) {
			return true
		}
	}
	return false
}
