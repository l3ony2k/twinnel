//go:build darwin

package darwin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/sysdns"
)

var fakeNS func(name string, args ...string) (string, error)

func TestSystemDNS(t *testing.T) {
	order, err := os.ReadFile(filepath.Join("testdata", "service_order.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// en0 (Wi-Fi) is active with an address; bridge0 (Thunderbolt
	// Bridge) is "status: inactive".
	ifc, err := os.ReadFile(filepath.Join("testdata", "ifconfig.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var ran []string
	d := SystemDNS{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		out, err := fakeNS(name, args...)
		return []byte(out), err
	}}
	fakeNS = func(name string, args ...string) (string, error) {
		cmd := filepath.Base(name) + " " + strings.Join(args, " ")
		ran = append(ran, cmd)
		switch cmd {
		case "networksetup -listnetworkserviceorder":
			return string(order), nil
		case "networksetup -getdnsservers Wi-Fi":
			return "192.0.2.53\n", nil
		case "ifconfig ":
			return string(ifc), nil
		}
		if strings.HasPrefix(cmd, "networksetup -getdnsservers") {
			return "There aren't any DNS Servers set on X.\n", nil
		}
		return "", nil
	}
	svcs, err := d.Services(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Disabled (USB LAN) and device-less (VPN) services are left alone.
	if len(svcs) != 2 || svcs[0].Name != "Thunderbolt Bridge" || svcs[0].Servers != nil || svcs[1].Servers[0] != "192.0.2.53" {
		t.Fatalf("%+v", svcs)
	}
	// Connected is the system-dns check's rule (sysdns.LinkConnected).
	if svcs[0].Connected || !svcs[1].Connected {
		t.Fatalf("connected: %+v", svcs)
	}
	ran = nil
	if err := d.Set(context.Background(), "Wi-Fi", []string{"127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(context.Background(), "Wi-Fi", nil); err != nil {
		t.Fatal(err)
	}
	if ran[0] != "networksetup -setdnsservers Wi-Fi 127.0.0.1" || ran[1] != "networksetup -setdnsservers Wi-Fi Empty" {
		t.Fatalf("%q", ran)
	}
	if err := d.Set(context.Background(), "Wi-Fi", []string{"; rm -rf /"}); err == nil {
		t.Fatal("accepted a non-address")
	}
}

// An inactive service (Thunderbolt Bridge, automatic DNS) does not hold
// dns.status at "not pointed" while the connected Wi-Fi points at mihomo:
// the same verdict as the system-dns check, which skips it.
func TestSystemDNSStatusSkipsInactive(t *testing.T) {
	order, err := os.ReadFile(filepath.Join("testdata", "service_order.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ifc, err := os.ReadFile(filepath.Join("testdata", "ifconfig.txt"))
	if err != nil {
		t.Fatal(err)
	}
	d := SystemDNS{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch filepath.Base(name) + " " + strings.Join(args, " ") {
		case "networksetup -listnetworkserviceorder":
			return order, nil
		case "ifconfig ":
			return ifc, nil
		case "networksetup -getdnsservers Wi-Fi":
			return []byte("127.0.0.1\n"), nil
		}
		return []byte("There aren't any DNS Servers set on X.\n"), nil
	}}
	m := &sysdns.Manager{P: d, Path: filepath.Join(t.TempDir(), "dns-backup.json"), Target: sysdns.TargetOf(d)}
	st, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Pointed || len(st.Unprotected) != 1 || st.Unprotected[0] != "Wi-Fi" {
		t.Fatalf("status: %+v", st)
	}
}
