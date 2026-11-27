package health

import "testing"

func TestTitleOf(t *testing.T) {
	zh, en := TitleOf("zh-Hans", "magicdns"), TitleOf("en", "magicdns")
	if zh == "" || en == "" || zh == en {
		t.Fatalf("magicdns titles %q %q", zh, en)
	}
	if TitleOf("en", "nope") != "" {
		t.Fatal("unknown id has a title")
	}
}
