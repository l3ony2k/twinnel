//go:build linux

package main

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/l3ony2k/twinnel/platform/linux"
	"github.com/l3ony2k/twinnel/platform/linux/systemd"
)

// The helper on Linux: twinnel-helper in /usr/local/libexec, run by the
// systemd unit twinnel-helper.service, its state in /var/lib/twinnel
// (`twinnel helper install`, through pkexec).
func init() {
	helperInstalledPaths = []string{linux.HelperBin, systemd.New().DefinitionPath(linux.HelperUnit)}
	helperStateDir = linux.StateDir
	openHelperLogHook = openHelperJournal
	appBundled = appInstalled
}

// openHelperJournal opens the helper's log file when this user can read
// it; else (it is root's) the unit's journal in a terminal, where
// systemd-journal, wheel or adm members can read it. A failure shows as
// a toast.
func openHelperJournal(a *app, path string) bool {
	if f, err := os.Open(path); err == nil {
		f.Close()
		return false // the file: opened in its default app as elsewhere
	}
	argv, err := linux.TerminalArgv(linux.HelperJournalArgv(), os.Getenv, exec.LookPath)
	if err == nil {
		cmd := exec.Command(argv[0], argv[1:]...)
		if err = cmd.Start(); err == nil {
			go cmd.Wait()
			return true
		}
	}
	a.toastErr = strings.Join(linux.HelperJournalArgv(), " ") + ": " + err.Error()
	a.invalidate()
	return true
}

// appInstalled: 登录时打开 (an XDG autostart entry naming this
// executable) is offered for an installed Twinnel, not for a `go run`
// build in the Go cache's temporary folder, which is gone after the run.
func appInstalled() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return !strings.HasPrefix(filepath.Base(filepath.Dir(exe)), "go-build") && !strings.Contains(exe, string(filepath.Separator)+"go-build")
}

func init() { groupGapProbe = probeGroupGap }

// probeGroupGap: the socket's group against this process's groups (what
// the kernel checks), and the group database for whether an install
// added the user already.
func probeGroupGap(socket string) groupGap {
	fi, err := os.Stat(socket)
	if err != nil {
		return groupGap{}
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return groupGap{}
	}
	groups, err := syscall.Getgroups()
	if err != nil {
		return groupGap{}
	}
	g := linux.SocketGroupGap(linux.SystemAccounts, user.LookupGroupId, int(st.Gid), uint32(fi.Mode().Perm()),
		syscall.Getegid(), groups, strconv.Itoa(os.Getuid()))
	return groupGap{group: g.Group, listed: g.Listed}
}
