package windows

import "testing"

func TestExeOf(t *testing.T) {
	for in, want := range map[string]string{
		`"C:\Program Files\Clash Verge\clash-verge.exe" --hidden`: `C:\Program Files\Clash Verge\clash-verge.exe`,
		`C:\Program Files\Clash Party\Clash Party.exe --autostart`: `C:\Program Files\Clash Party\Clash Party.exe`,
		`mihomo -d x`: "mihomo",
	} {
		if got := exeOf(in); got != want {
			t.Errorf("exeOf(%q) = %q", in, got)
		}
	}
}
