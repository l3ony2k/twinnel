package foreign

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
)

// ProcPortOwner finds the process listening on TCP port through a /proc
// tree at root ("/proc" on Linux; tests use a temp dir): the listening
// socket's inode from net/tcp(6), then the process whose fd links to it.
// It returns nil, nil when nothing listens or the owner is not visible.
func ProcPortOwner(ctx context.Context, root string, port int) (*Process, error) {
	var inodes []string
	for _, t := range []string{"net/tcp", "net/tcp6"} {
		b, err := os.ReadFile(filepath.Join(root, t))
		if err != nil {
			continue
		}
		inodes = append(inodes, ProcNetListenInodes(string(b), port)...)
	}
	if len(inodes) == 0 {
		return nil, nil
	}
	want := map[string]bool{}
	for _, in := range inodes {
		if in != "0" {
			want["socket:["+in+"]"] = true
		}
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		fds, err := os.ReadDir(filepath.Join(root, e.Name(), "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			l, err := os.Readlink(filepath.Join(root, e.Name(), "fd", fd.Name()))
			if err == nil && want[l] {
				return procProcess(root, pid), nil
			}
		}
	}
	return nil, nil
}

func procProcess(root string, pid int) *Process {
	dir := filepath.Join(root, strconv.Itoa(pid))
	p := &Process{PID: pid}
	p.Exe, _ = os.Readlink(filepath.Join(dir, "exe"))
	p.Cwd, _ = os.Readlink(filepath.Join(dir, "cwd"))
	if b, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil {
		p.Args = ProcCmdline(b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "stat")); err == nil {
		p.PPID = ProcStatPPID(string(b))
	}
	if p.PPID > 1 {
		p.ParentExe, _ = os.Readlink(filepath.Join(root, strconv.Itoa(p.PPID), "exe"))
	}
	return p
}
