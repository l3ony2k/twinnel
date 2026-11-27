package linux

import (
	"bytes"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
)

func planText(steps []service.Step) string {
	var b bytes.Buffer
	service.FormatPlan(&b, steps)
	return b.String()
}

func TestHelperInstallPlan(t *testing.T) {
	steps, err := HelperInstallPlan("/tmp/build/twinnel-helper", store.DefaultDir, rpc.DefaultSocket, store.DefaultHelperLabel, "sudo", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := planText(steps)
	t.Log("\n" + out)
	for _, s := range []string{
		"install -m 0755 /tmp/build/twinnel-helper /usr/local/libexec/twinnel-helper",
		"install -d -m 0755 /var/lib/twinnel",
		"install -d -m 0755 /var/run/twinnel",
		"/etc/systemd/system/twinnel-helper.service",
		"--state-dir /var/lib/twinnel",
		"--group sudo",
		"daemon-reload",
		"enable twinnel-helper",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("%q not in plan:\n%s", s, out)
		}
	}
	if strings.Contains(out, "Library") || strings.Contains(out, "launchctl") || strings.Contains(out, "groupadd") || strings.Contains(out, "append:") {
		t.Errorf("macOS paths, groupadd or log files in plan:\n%s", out)
	}

	steps, _ = HelperInstallPlan("/b/twinnel-helper", "/srv/twinnel", rpc.DefaultSocket, "my-helper", "twinnel", false,
		[]service.Step{AddToGroupStep("alice", "twinnel")})
	out = planText(steps)
	// The group first, then the installing user in it.
	if g, u := strings.Index(out, "groupadd --system twinnel"), strings.Index(out, "usermod -a -G twinnel alice"); g < 0 || u < g {
		t.Errorf("groupadd then usermod:\n%s", out)
	}
	if !strings.Contains(out, "/usr/sbin/groupadd --system twinnel") || !strings.Contains(out, "--state-dir /srv/twinnel") || !strings.Contains(out, "my-helper.service") {
		t.Errorf("plan:\n%s", out)
	}

	steps, err = HelperUninstallPlan(store.DefaultDir, rpc.DefaultSocket, store.DefaultHelperLabel)
	if err != nil {
		t.Fatal(err)
	}
	out = planText(steps)
	if !strings.Contains(out, "disable --now twinnel-helper") || !strings.Contains(out, "rm -f /usr/local/libexec/twinnel-helper") || strings.Contains(out, "/var/lib/twinnel") {
		t.Errorf("uninstall plan:\n%s", out)
	}
}
