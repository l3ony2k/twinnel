package version

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Binary runs `<bin> --version` ("twinnel-helper 1.2.3") and returns the
// version, for the helper binaries on disk (bundled, installed, the
// Windows service host). Reads only.
func Binary(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", bin, err)
	}
	return ParseBinary(bin, string(out))
}

// ParseBinary reads `--version` output: its last word.
func ParseBinary(bin, out string) (string, error) {
	f := strings.Fields(out)
	if len(f) < 2 {
		return "", fmt.Errorf("%s --version: unexpected output %q", bin, strings.TrimSpace(out))
	}
	return f[len(f)-1], nil
}
