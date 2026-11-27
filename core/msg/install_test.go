package msg

import (
	"fmt"
	"testing"

	"github.com/l3ony2k/twinnel/core/kernel"
)

// The install errors come from the real producers, so a reworded one
// fails here.
func TestInstallErrors(t *testing.T) {
	for _, tt := range []struct {
		text string
		code string
		args []string
	}{
		{(&kernel.MSIExitError{Code: 1618}).Error(), "msi-busy", nil},
		{(&kernel.MSIExitError{Code: 1602}).Error(), "msi-canceled", nil},
		{(&kernel.MSIExitError{Code: 1625}).Error(), "msi-blocked", nil},
		{(&kernel.MSIExitError{Code: 1638}).Error(), "msi-other-version", nil},
		{(&kernel.MSIExitError{Code: 1603, Log: `C:\ProgramData\Twinnel\tailscale-msi.log`}).Error(), "msi-failed", []string{"1603", `C:\ProgramData\Twinnel\tailscale-msi.log`}},
		{fmt.Errorf("%w: exit status 1", kernel.ErrDoesNotRun).Error(), "mihomo-no-run", nil},
		{"Tailscale was installed but its service did not start; restart the computer and try again", "ts-service-down", nil},
		{`Tailscale was installed but C:\Program Files\Tailscale\tailscale.exe does not run: exit status 1`, "ts-cli-no-run", []string{`C:\Program Files\Tailscale\tailscale.exe`}},
		{"pkgs.tailscale.com has no Windows installer for arm64", "ts-no-installer", []string{"arm64"}},
		{"release v1.19.34 has no asset for windows/arm", "mihomo-no-build", []string{"v1.19.34", "windows/arm"}},
		{"checksum mismatch: got sha256:00, want sha256:11", "download-damaged", nil},
	} {
		m := Parse(tt.text)
		if m.Code != tt.code || tt.args != nil && fmt.Sprint(m.Args) != fmt.Sprint(tt.args) {
			t.Errorf("Parse(%q) = %+v, want %s %q", tt.text, m, tt.code, tt.args)
		}
	}
}
