package overrides

import (
	"bytes"
	"errors"
)

// BaseTemplate is the built-in base layer's template.
const BaseTemplate = "base-tun-fakeip.yaml"

// mixedStack is the template's TUN stack line, and gvisorStack its
// Windows replacement.
var (
	mixedStack  = []byte("\n  stack: mixed\n")
	gvisorStack = []byte("\n  # Windows: gvisor. The mixed stack (system TCP) was seen to drop every TCP\n" +
		"  # connection through the TUN on Windows 11 while ICMP and the proxy port\n" +
		"  # worked (docs/tun-dataplane.md). gvisor handles TCP in user space.\n" +
		"  stack: gvisor\n")
)

// Base returns the built-in base layer for an OS (runtime.GOOS): the
// template, with tun.stack gvisor on Windows. A stack the user sets in an
// override of their own still wins (user overrides run after the base),
// and an existing base layer is never rewritten (setup only writes one
// when there is none).
func Base(goos string) ([]byte, error) {
	b, err := Templates.ReadFile(BaseTemplate)
	if err != nil {
		return nil, err
	}
	if goos != "windows" {
		return b, nil
	}
	if bytes.Count(b, mixedStack) != 1 {
		return nil, errors.New("base template: no single \"stack: mixed\" line to replace")
	}
	return bytes.Replace(b, mixedStack, gvisorStack, 1), nil
}
