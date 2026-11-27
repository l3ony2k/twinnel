package foreign

import (
	"context"
	"strconv"
	"strings"
)

// Runner runs a read-only command and returns its standard output.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// LsofPortOwner finds the process listening on TCP port with lsof and ps
// (macOS; the root helper sees every process). nil, nil when nothing
// listens.
func LsofPortOwner(ctx context.Context, run Runner, port int) (*Process, error) {
	out, err := run(ctx, "/usr/sbin/lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fp")
	if err != nil && len(out) == 0 {
		// lsof exits 1 when nothing matches.
		return nil, nil
	}
	pids := ParseLsofPIDs(string(out))
	if len(pids) == 0 {
		return nil, nil
	}
	p := &Process{PID: pids[0]}
	ps := strconv.Itoa(p.PID)
	if b, err := run(ctx, "/bin/ps", "-ww", "-o", "ppid=,comm=", "-p", ps); err == nil {
		p.PPID, p.Exe = ParsePSPPIDComm(string(b))
	}
	if b, err := run(ctx, "/bin/ps", "-ww", "-o", "args=", "-p", ps); err == nil {
		// Split at spaces: good enough to find -d and the client markers.
		p.Args = strings.Fields(firstLine(string(b)))
	}
	if b, err := run(ctx, "/usr/sbin/lsof", "-a", "-p", ps, "-d", "cwd", "-Fn"); err == nil {
		p.Cwd = ParseLsofName(string(b))
	}
	if p.PPID > 1 {
		if b, err := run(ctx, "/bin/ps", "-ww", "-o", "ppid=,comm=", "-p", strconv.Itoa(p.PPID)); err == nil {
			_, p.ParentExe = ParsePSPPIDComm(string(b))
		}
	}
	return p, nil
}
