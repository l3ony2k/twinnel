package winpath

import "testing"

func TestJoinDir(t *testing.T) {
	if got := Join(`C:\Program Files\`, "Twinnel", `\x.exe`); got != `C:\Program Files\Twinnel\x.exe` {
		t.Errorf("Join = %q", got)
	}
	if got := Join("C:/a", "b/c"); got != `C:\a\b\c` {
		t.Errorf("Join = %q", got)
	}
	if got := Dir(`C:\a\b.exe`); got != `C:\a` {
		t.Errorf("Dir = %q", got)
	}
}

func TestDefaults(t *testing.T) {
	t.Setenv("ProgramFiles", `D:\Apps`)
	t.Setenv("ProgramData", "relative") // not absolute: ignored
	if HelperBin() != `D:\Apps\Twinnel\twinnel-helper.exe` || StateDir() != `C:\ProgramData\Twinnel` {
		t.Errorf("%s %s", HelperBin(), StateDir())
	}
	if TailscaleCLI() != `D:\Apps\Tailscale\tailscale.exe` {
		t.Error(TailscaleCLI())
	}
}
