package i18n

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/msg"
	"github.com/l3ony2k/duet/core/rpc"
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
		upgrade DOMAIN SUFFIX KEYWORD example`) {
		allowedASCII[strings.ToLower(w)] = true
	}
}

// pendingElsewhere are texts other work removes (design review U1, U58:
// batch B deletes the env-var hints). Drop entries as they go.
var pendingElsewhere = map[string]bool{
	"zh.AboutStrings.FAQReadOnlyA": true,
	"zh.WritesDisabledHint":        true,
	"zh.HelperNotRunningHint":      true,
}

var asciiWord = regexp.MustCompile(`[A-Za-z]{4,}`)

// zhTexts walks every string in a Strings value.
func walkStrings(v reflect.Value, path string, fn func(path, s string)) {
	switch v.Kind() {
	case reflect.String:
		fn(path, v.String())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			walkStrings(v.Field(i), path+"."+v.Type().Field(i).Name, fn)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkStrings(v.Index(i), path, fn)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			walkStrings(v.MapIndex(k), path+"["+k.String()+"]", fn)
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			walkStrings(v.Elem(), path, fn)
		}
	}
}

func englishWords(s string) []string {
	// Placeholders and config keys in quotes or code spans are syntax.
	s = regexp.MustCompile("`[^`]*`|%\\[?\\d*\\]?[a-z]|[\\w.-]+\\.(yaml|yml|json|plist|app|sh)\\b|[a-zA-Z][a-zA-Z0-9]*([-_][a-zA-Z0-9]+)+|[\\w-]+(\\.[\\w-]+)+/?\\S*|https?://\\S+").ReplaceAllString(s, " ")
	var out []string
	for _, w := range asciiWord.FindAllString(s, -1) {
		if !allowedASCII[strings.ToLower(w)] {
			out = append(out, w)
		}
	}
	return out
}

func TestNoEnglishInChinese(t *testing.T) {
	zh := For("zh")
	var bad []string
	walkStrings(reflect.ValueOf(*zh), "zh", func(path, s string) {
		if pendingElsewhere[path] {
			return
		}
		if w := englishWords(s); len(w) > 0 {
			bad = append(bad, path+": "+strings.Join(w, " ")+"  ← "+s)
		}
	})
	for _, code := range msg.Codes() { // every code has zh words (the templates are walked above)
		if _, ok := zh.Msg(rpc.Msg{Code: code}); !ok {
			bad = append(bad, "msg "+code+": no text")
		}
	}
	for code := range zh.HistReasons {
		if w := englishWords(zh.HistoryReasonText(code, []string{"20261127T060539Z-52d20aaa"}, "")); len(w) > 0 {
			bad = append(bad, "history "+code+": "+strings.Join(w, " "))
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

func TestEnglishWordsCatches(t *testing.T) {
	if w := englishWords("错误：layer app3-demo: needs a list, got scalar"); strings.Join(w, " ") != "layer needs list scalar" {
		t.Errorf("englishWords = %q", w)
	}
	if w := englishWords("用 brew upgrade tailscale 更新 dns.fake-ip-filter-mode"); len(w) != 0 {
		t.Errorf("syntax counted: %q", w)
	}
}
