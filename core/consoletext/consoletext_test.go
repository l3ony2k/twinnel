package consoletext

import (
	"runtime"
	"testing"
)

func TestDecodeKeepsUTF8(t *testing.T) {
	for _, s := range []string{"", "Ok.", "找不到元素。\r\n", "出口 IP 实测（只发 GET）"} {
		if got := string(Decode([]byte(s))); got != s {
			t.Errorf("%q → %q", s, got)
		}
	}
}

// Invalid UTF-8 (a GBK "找不到" from netsh on a Chinese system) is left as
// is where there is no OEM code page; Windows converts it (the code page
// depends on the machine, so only that it is valid UTF-8 now is checked).
func TestDecodeInvalid(t *testing.T) {
	gbk := []byte{0xd5, 0xd2, 0xb2, 0xbb, 0xb5, 0xbd}
	got := Decode(gbk)
	if runtime.GOOS != "windows" {
		if string(got) != string(gbk) {
			t.Fatalf("changed off Windows: %q", got)
		}
		return
	}
	for _, r := range string(got) {
		if r == '�' {
			t.Fatalf("not converted: %q", got)
		}
	}
}
