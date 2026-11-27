package i18n

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/msg"
	"github.com/l3ony2k/duet/core/rpc"
)

// pendingElsewhere are texts other work still has to change (design
// review U1: the FAQ answer names the old env gate). Drop entries as they
// go.
var pendingElsewhere = map[string]bool{
	"zh.AboutStrings.FAQReadOnlyA": true,
}

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

func TestNoEnglishInChinese(t *testing.T) {
	zh := For("zh")
	var bad []string
	walkStrings(reflect.ValueOf(*zh), "zh", func(path, s string) {
		if pendingElsewhere[path] {
			return
		}
		if w := EnglishWords(s); len(w) > 0 {
			bad = append(bad, path+": "+strings.Join(w, " ")+"  ← "+s)
		}
	})
	for _, code := range msg.Codes() { // every code has zh words (the templates are walked above)
		if _, ok := zh.Msg(rpc.Msg{Code: code}); !ok {
			bad = append(bad, "msg "+code+": no text")
		}
	}
	for code := range zh.HistReasons {
		if w := EnglishWords(zh.HistoryReasonText(code, []string{"20261127T060539Z-52d20aaa"}, "")); len(w) > 0 {
			bad = append(bad, "history "+code+": "+strings.Join(w, " "))
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

func TestEnglishWordsCatches(t *testing.T) {
	if w := EnglishWords("错误：layer app3-demo: needs a list, got scalar"); strings.Join(w, " ") != "layer needs list scalar" {
		t.Errorf("englishWords = %q", w)
	}
	if w := EnglishWords("用 brew upgrade tailscale 更新 dns.fake-ip-filter-mode"); len(w) != 0 {
		t.Errorf("syntax counted: %q", w)
	}
}
