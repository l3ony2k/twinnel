package overrides

import (
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/config"
	"gopkg.in/yaml.v3"
)

func stackOf(t *testing.T, b []byte) string {
	t.Helper()
	out, err := config.MergeBytes(nil, b)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		TUN struct {
			Stack string `yaml:"stack"`
		} `yaml:"tun"`
	}
	if err := yaml.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	return v.TUN.Stack
}

func TestBaseStackPerOS(t *testing.T) {
	for goos, want := range map[string]string{"windows": "gvisor", "darwin": "mixed", "linux": "mixed"} {
		b, err := Base(goos)
		if err != nil {
			t.Fatal(err)
		}
		if got := stackOf(t, b); got != want {
			t.Errorf("%s: stack %q, want %q", goos, got, want)
		}
	}
	w, _ := Base("windows")
	d, _ := Base("darwin")
	if strings.Count(string(w), "\n")-strings.Count(string(d), "\n") != 3 {
		t.Error("the Windows base should differ by the stack line and its comment only")
	}
}

// A stack set in a user override wins over the base's.
func TestUserStackWins(t *testing.T) {
	b, _ := Base("windows")
	out, err := config.Compose([]byte("proxies: []\n"), []config.Layer{
		config.OverrideLayer("base", "Base", config.KindUser, config.StageBase, b),
		config.OverrideLayer("mine", "Mine", config.KindUser, config.StageUser, []byte("tun:\n  stack: system\n")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "stack: system") {
		t.Fatalf("user stack lost:\n%s", out)
	}
}
