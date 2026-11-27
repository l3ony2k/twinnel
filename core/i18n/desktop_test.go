package i18n

import (
	"runtime"
	"strings"
	"testing"
)

// The Linux wording replaces the shared maps instead of changing them
// (the glossary test, glossary_test.go, checks its words).
func TestLinuxWordingCopiesMaps(t *testing.T) {
	before := en.MsgTexts["refuses-exposed"]
	var l Lang
	for _, x := range Langs() {
		if x.Tag == "en" {
			l = x
		}
	}
	s := wordedFor(t, l, Linux)
	if en.MsgTexts["refuses-exposed"] != before || runtime.GOOS != "linux" && strings.Contains(en.HistReasons["setup"], "computer") {
		t.Fatal("the Linux wording changed a shared map")
	}
	if strings.Contains(s.MsgTexts["refuses-exposed"], "Mac") || s.HistReasons["setup"] != "Set up this computer" {
		t.Fatal("the Linux table is not worded")
	}
}
