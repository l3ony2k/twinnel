package status

import "testing"

func TestStripFlags(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"NODE-US-01", "NODE-US-01"},
		{"🇺🇸 NODE-US-01", "NODE-US-01"},
		{"🇺🇸NODE", "NODE"},
		{"HK 🇭🇰 01", "HK 01"},
		{"香港 01 🇭🇰", "香港 01"},
		{"🇺🇸 | 美国 01", "美国 01"},
		{"🇯🇵", "JP"},
		{"🇯🇵🇺🇸", "JPUS"},
		{"🏴\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F London", "London"},
		{"🇺🇸️ US", "US"},
		{"DIRECT", "DIRECT"},
		{"", ""},
	} {
		if got := StripFlags(tc.in); got != tc.want {
			t.Errorf("StripFlags(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if HasFlag("abc") || !HasFlag("🇺🇸 x") {
		t.Error("HasFlag")
	}
}
