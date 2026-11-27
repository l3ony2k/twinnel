package store

import (
	"bytes"
	"os"
	"testing"
)

// State files are UTF-8 on every OS: Chinese text is written as its UTF-8
// bytes, never as "?" or a code-page encoding, and reads back the same.
// (QA once saw "?? IP ??" in health-egress.json on Windows: that was the
// file shown through a non-UTF-8 console, which also turns "（" into "(";
// the bytes on disk are these.)
func TestWriteJSONIsUTF8(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in := map[string]string{"title": "出口 IP 实测", "evidence": "测试地址（只发 GET，不带任何个人数据）"}
	if err := s.WriteJSON("health-egress.json", in); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.Path("health-egress.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"出口 IP 实测", "（只发 GET，不带任何个人数据）"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("file lacks %q as UTF-8:\n%s", want, b)
		}
	}
	if bytes.Contains(b, []byte("?")) || bytes.HasPrefix(b, []byte{0xef, 0xbb, 0xbf}) {
		t.Errorf("file has '?' or a BOM:\n%s", b)
	}
	var out map[string]string
	if err := s.ReadJSON("health-egress.json", &out); err != nil || out["title"] != in["title"] {
		t.Fatal(out, err)
	}
}
