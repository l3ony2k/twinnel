package health

import (
	"strings"
	"testing"
	"time"
)

// A report shown from the helper's last run says how old it is, in both
// languages and both renderings; a run made for the request says nothing.
func TestReportAgeNote(t *testing.T) {
	for _, tc := range []struct {
		lang string
		age  time.Duration
		want string
	}{
		{"en", 23 * time.Second, "The helper's last run, 23 s ago"},
		{"en", 1500 * time.Millisecond, "2 s ago"},
		{"en", 90 * time.Second, "1 min ago"},
		{"zh-Hans", 23 * time.Second, "后台助手 23 秒前的检查结果"},
		{"", 3 * time.Minute, "后台助手 3 分钟前的检查结果"},
	} {
		r := &Report{Lang: tc.lang, Source: SourceHelper, Age: tc.age}
		for _, out := range []string{r.Text(), r.Markdown()} {
			if !strings.Contains(out, tc.want) {
				t.Errorf("%s %v: %q lacks %q", tc.lang, tc.age, out, tc.want)
			}
		}
	}
	r := &Report{Lang: "en", Source: SourceHelper}
	if strings.Contains(r.Text(), "last run") {
		t.Errorf("a run now says it is the last one: %s", r.Text())
	}
}

func TestMatchLang(t *testing.T) {
	for in, want := range map[string]string{"": DefaultLang, "en-GB": "en", "zh": "zh-Hans", "fr": DefaultLang} {
		if got := MatchLang(in); got != want {
			t.Errorf("MatchLang(%q) = %q, want %q", in, got, want)
		}
	}
}
