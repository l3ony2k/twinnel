//go:build unix

package kernel

import (
	"io/fs"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
)

func fileOwner(fi fs.FileInfo) (uid, gid int, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}

func lookupCredential(uid int) (Credential, error) {
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return Credential{}, err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return Credential{}, err
	}
	return Credential{UID: uid, GID: gid, Home: u.HomeDir, Name: u.Username}, nil
}

// dropTo makes cmd run as c with no supplementary groups and a minimal
// environment that does not point at root's home.
func dropTo(cmd *exec.Cmd, c Credential) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(c.UID), Gid: uint32(c.GID), Groups: []uint32{}}
	home := c.Home
	if home == "" {
		home = "/var/empty"
	}
	env := []string{
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=" + home,
		"USER=" + c.Name,
		"LOGNAME=" + c.Name,
		"TMPDIR=/tmp/",
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "SAFE_PATHS=") {
			env = append(env, kv) // mihomo -t needs it
		}
	}
	cmd.Env = env
}

// DropTo makes cmd run as c (exported for callers that build their own
// exec.Cmd, e.g. mihomo -t).
func DropTo(cmd *exec.Cmd, c Credential) { dropTo(cmd, c) }
