//go:build darwin

package darwin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemDNS(t *testing.T) {
	order, err := os.ReadFile(filepath.Join("testdata", "service_order.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var ran []string
	d := SystemDNS{Run: func(_ context.Context, name string, args ...string) (string, error) {
		cmd := filepath.Base(name) + " " + strings.Join(args, " ")
		ran = append(ran, cmd)
		switch cmd {
		case "networksetup -listnetworkserviceorder":
			return string(order), nil
		case "networksetup -getdnsservers Wi-Fi":
			return "192.0.2.53\n", nil
		}
		if strings.HasPrefix(cmd, "networksetup -getdnsservers") {
			return "There aren't any DNS Servers set on X.\n", nil
		}
		return "", nil
	}}
	svcs, err := d.Services(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Disabled (USB LAN) and device-less (VPN) services are left alone.
	if len(svcs) != 2 || svcs[0].Name != "Thunderbolt Bridge" || svcs[0].Servers != nil || svcs[1].Servers[0] != "192.0.2.53" {
		t.Fatalf("%+v", svcs)
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
