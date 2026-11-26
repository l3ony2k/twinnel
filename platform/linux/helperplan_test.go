package linux

import (
	"bytes"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/core/store"
)

func planText(steps []service.Step) string {
	var b bytes.Buffer
	service.FormatPlan(&b, steps)
	return b.String()
}

func TestHelperInstallPlan(t *testing.T) {
	steps, err := HelperInstallPlan("/tmp/build/duet-helper", store.DefaultDir, rpc.DefaultSocket, store.DefaultHelperLabel, "sudo", true)
	if err != nil {
		t.Fatal(err)
	}
	out := planText(steps)
	t.Log("\n" + out)
	for _, s := range []string{
		"install -m 0755 /tmp/build/duet-helper /usr/local/libexec/duet-helper",
		"install -d -m 0755 /var/lib/duet",
		"install -d -m 0755 /var/run/duet",
		"/etc/systemd/system/duet-helper.service",
		"--state-dir /var/lib/duet",
		"--group sudo",
		"daemon-reload",
		"enable duet-helper",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("%q not in plan:\n%s", s, out)
		}
	}
	if strings.Contains(out, "Library") || strings.Contains(out, "launchctl") || strings.Contains(out, "groupadd") || strings.Contains(out, "append:") {
		t.Errorf("macOS paths, groupadd or log files in plan:\n%s", out)
	}

	steps, _ = HelperInstallPlan("/b/duet-helper", "/srv/duet", rpc.DefaultSocket, "my-helper", "duet", false)
	out = planText(steps)
	if !strings.Contains(out, "/usr/sbin/groupadd --system duet") || !strings.Contains(out, "--state-dir /srv/duet") || !strings.Contains(out, "my-helper.service") {
		t.Errorf("plan:\n%s", out)
	}

	steps, err = HelperUninstallPlan(store.DefaultDir, rpc.DefaultSocket, store.DefaultHelperLabel)
	if err != nil {
		t.Fatal(err)
	}
	out = planText(steps)
	if !strings.Contains(out, "disable --now duet-helper") || !strings.Contains(out, "rm -f /usr/local/libexec/duet-helper") || strings.Contains(out, "/var/lib/duet") {
		t.Errorf("uninstall plan:\n%s", out)
	}
}
