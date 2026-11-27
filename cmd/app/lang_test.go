package main

import (
	"testing"

	"github.com/l3ony2k/duet/core/i18n"
)

// Every UI language has a layout, and the 语言 choices list 跟随系统 then
// every language in its own name.
func TestLangLayouts(t *testing.T) {
	for _, l := range i18n.Langs() {
		if langLayouts[l.Tag].label < 96 {
			t.Errorf("no layout for %s", l.Tag)
		}
	}
	labels, values := langOptions()
	if len(labels) != len(i18n.Langs())+1 || values[0] != "" || labels[1] != "简体中文" || labels[2] != "English" {
		t.Errorf("options %q %q", labels, values)
	}
	defer func() { i18n.Set("zh-Hans"); setLayout("zh-Hans") }()
	setLayout("en")
	if labelWidth != langLayouts["en"].label || halfCardMin == 0 {
		t.Error("setLayout(en)")
	}
}
