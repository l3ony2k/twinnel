package main

import (
	"testing"

	"github.com/l3ony2k/twinnel/core/i18n"
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

// A label wider than the language's column widens it for every row (up
// to labelMax) instead of being cut; a new language starts over.
func TestLabelColumnGrows(t *testing.T) {
	defer func() { setLayout("zh-Hans") }()
	setLayout("zh-Hans")
	if growLabel(labelWidth-10) || labelCol() != labelWidth {
		t.Fatal("a narrower label changed the column")
	}
	if !growLabel(130) || labelCol() != 130 {
		t.Fatalf("column %v, want 130", labelCol())
	}
	if growLabel(120) || labelCol() != 130 {
		t.Fatal("the column shrank")
	}
	if !growLabel(999) || labelCol() != labelMax {
		t.Fatalf("column %v, want the cap", labelCol())
	}
	setLayout("en")
	if labelCol() != langLayouts["en"].label {
		t.Fatalf("setLayout kept the grown column: %v", labelCol())
	}
}
