package legacy

import (
	"strings"
	"testing"
)

func TestApplyEnvAliases(t *testing.T) {
	env := map[string]string{"TWINNEL_LANG": "zh-Hans"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	set := func(k, v string) error { env[k] = v; return nil }
	got := applyEnvAliases([]string{"DUET_SOCKET=/tmp/x.sock", "DUET_LANG=en", "DUET_=x", "PATH=/bin", "DUET_READ_ONLY=1"}, lookup, set)
	if strings.Join(got, ",") != "DUET_READ_ONLY,DUET_SOCKET" {
		t.Errorf("copied %v", got)
	}
	if env["TWINNEL_SOCKET"] != "/tmp/x.sock" || env["TWINNEL_READ_ONLY"] != "1" {
		t.Errorf("env %v", env)
	}
	if env["TWINNEL_LANG"] != "zh-Hans" {
		t.Errorf("a set TWINNEL_ name was overwritten: %q", env["TWINNEL_LANG"])
	}
	if _, ok := env["TWINNEL_"]; ok {
		t.Error("bare prefix copied")
	}
	if n := EnvNotice(got); !strings.Contains(n, "DUET_READ_ONLY, DUET_SOCKET") {
		t.Errorf("notice %q", n)
	}
	if EnvNotice(nil) != "" {
		t.Error("notice without names")
	}
}

// The old macOS paths derive from the old labels.
func TestPaths(t *testing.T) {
	if HelperTools != "/Library/PrivilegedHelperTools/com.l3ony2k.duet.helper" || HelperPlist != "/Library/LaunchDaemons/com.l3ony2k.duet.helper.plist" {
		t.Error(HelperTools, HelperPlist)
	}
}
