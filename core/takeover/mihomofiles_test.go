package takeover

import "testing"

func TestMihomoServiceAndFiles(t *testing.T) {
	svcs := []ServiceDef{
		{Label: "sh.example.tailscale", Program: []string{"/opt/example/bin/tailscaled"}},
		{Label: "empty"},
		{Label: "com.example.mihomo", Path: "/Library/LaunchDaemons/com.example.mihomo.plist",
			Program: []string{"/Users/example/mh/bin/mihomo", "-d", "/Users/example/mh/conf"}},
	}
	svc := MihomoService(svcs)
	if svc == nil || svc.Label != "com.example.mihomo" {
		t.Fatalf("service = %+v", svc)
	}
	bin, dir, cfg := MihomoFiles(svc, "/Users/example")
	if bin != "/Users/example/mh/bin/mihomo" || dir != "/Users/example/mh/conf" || cfg != "/Users/example/mh/conf/config.yaml" {
		t.Fatalf("files = %q %q %q", bin, dir, cfg)
	}
	if MihomoService(svcs[:2]) != nil {
		t.Fatal("found a mihomo service where there is none")
	}
	bin, dir, cfg = MihomoFiles(nil, "/Users/example")
	if bin != "" || dir != "/Users/example/.config/mihomo" || cfg != "/Users/example/.config/mihomo/config.yaml" {
		t.Fatalf("fallback = %q %q %q", bin, dir, cfg)
	}
}
