//go:build !windows

package windows

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/l3ony2k/twinnel/core/health"
)

// NewSys returns a Sys that fails: the network facts come from iphlpapi and
// the registry, which only exist on Windows. Tests pass their own Sys.
func NewSys() Sys { return noSys{} }

type noSys struct{}

func errNoSys() error {
	return fmt.Errorf("windows network facts on %s: %w", runtime.GOOS, health.ErrNotSupported)
}

func (noSys) Adapters(context.Context) ([]Adapter, error) { return nil, errNoSys() }
func (noSys) Routes(context.Context) ([]RouteRow, error)  { return nil, errNoSys() }
func (noSys) NRPT(context.Context) ([]NRPTRule, error)    { return nil, errNoSys() }
func (noSys) DNSSource(string) string                     { return "" }

func hideWindow(*exec.Cmd) {}
