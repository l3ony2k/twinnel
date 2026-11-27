package linux

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
)

// The release archives ship packaging/linux/twinnel-helper.service for
// reading: it must be the unit `twinnel helper install` writes on a host
// whose socket group is "sudo". TWINNEL_UPDATE_GOLDEN=1 rewrites it.
func TestPackagedHelperUnit(t *testing.T) {
	steps, err := HelperInstallPlan("/tmp/build/twinnel-helper", store.DefaultDir, rpc.DefaultSocket, store.DefaultHelperLabel, "sudo", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	var unit []byte
	for _, s := range steps {
		if s.Kind == service.StepWrite && filepath.Ext(s.Path) == ".service" {
			unit = s.Content
		}
	}
	if unit == nil {
		t.Fatal("no unit file in the install plan")
	}
	header := []byte("# Reference copy of the unit `sudo ./install.sh` (twinnel helper install)\n" +
		"# writes to /etc/systemd/system/twinnel-helper.service. You do not need to\n" +
		"# copy it by hand. --group is sudo, else wheel, else a new twinnel group.\n")
	want := append(header, unit...)
	golden := filepath.Join("..", "..", "packaging", "linux", "twinnel-helper.service")
	if os.Getenv("TWINNEL_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is out of date; run TWINNEL_UPDATE_GOLDEN=1 go test ./platform/linux\n--- got\n%s\n--- want\n%s", golden, got, want)
	}
}
